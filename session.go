package dao

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/afocus/trace"
)

type Session struct {
	dao    *Dao
	indexs []string
	table  string
	fields []string
	cond   *sessionCond
	parts  []expPart
	tx     *sql.Tx

	ctx          context.Context
	querySrcData *sqlData
}

type sqlData struct {
	Query string
	Args  []interface{}
}

type expPart struct {
	name   string
	values []string
}

type Expression string

func ExpPart(name string, v ...string) expPart {
	return expPart{name: name, values: v}
}

func (e *expPart) String() string {
	return fmt.Sprintf(" %s %s", e.name, strings.Join(e.values, ", "))
}

var sessions = &sync.Pool{
	New: func() interface{} {
		return &Session{}
	},
}

func (s *Session) Close() {
	s.reset()
	sessions.Put(s)
}

func (s *Session) Table(t string) *Session {
	s.table = t
	return s
}

func (s *Session) UseIndex(index ...string) *Session {
	for k, v := range index {
		if !strings.HasPrefix(v, "`") {
			index[k] = "`" + v + "`"
		}
	}
	if s.indexs == nil {
		s.indexs = index
	} else {
		s.indexs = append(s.indexs, index...)
	}
	return s
}

func (s *Session) Where(query string, args ...interface{}) *Session {
	if s.cond != nil {
		return s.And(query, args...)
	}
	s.cond = newSessionCond(query, args...)
	return s
}

func (s *Session) And(query string, args ...interface{}) *Session {
	s.cond.And(query, args...)
	return s
}

func (s *Session) Or(query string, args ...interface{}) *Session {
	s.cond.Or(query, args...)
	return s
}

func (s *Session) Cols(field ...string) *Session {
	s.fields = field
	return s
}

func (s *Session) reset() {
	s.fields = nil
	s.parts = nil
	s.cond = nil
	s.indexs = nil
	s.table = ""
	s.querySrcData = nil
}

func (s *Session) Exec(query string, args ...interface{}) (int64, error) {
	defer s.reset()
	var (
		ret sql.Result
		err error
	)
	e, _ := trace.Start(
		s.ctx,
		"Dao Exec "+s.table,
		trace.Attribute("sql", query),
		trace.Attribute("args", args),
	)
	if s.tx != nil {
		ret, err = s.tx.Exec(query, args...)
	} else {
		ret, err = s.dao.DB().Exec(query, args...)
	}
	if err != nil {
		e.End(err)
		return 0, err
	}
	ra, err := ret.RowsAffected()
	e.SetAttributes(
		trace.Attribute("rowsAffected", ra),
	)
	e.End()
	return ra, err
}

func (s *Session) Insert(obj interface{}) (int64, error) {
	query, args, err := s.insertBuilder(s.table, s.fields, obj)
	if err != nil {
		return 0, err
	}
	return s.Exec(query, args...)
}

func (s *Session) insertBuilder(table string, updatefields []string, obj interface{}) (string, []interface{}, error) {

	var (
		fields []string
		args   []interface{}
		skipPk = -1
		sqltpl = "insert into %s(%s) values (%s)"
		v      = reflect.Indirect(reflect.ValueOf(obj))
	)

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	switch v.Type().Kind() {
	case reflect.Struct:
		fields, args = parseStruct(v)
	case reflect.Map:
		fields, args = parseMap(v)
	default:
		return "", nil, errorParseData
	}

	quto := make([]string, len(fields))
	for i, v := range fields {
		quto[i] = "?"
		fields[i] = fmt.Sprintf("`%s`", v)
		if v == "id" {
			if args[i] == nil {
				skipPk = i
				continue
			}
			if reflect.ValueOf(args[i]).IsZero() {
				skipPk = i
				continue
			}
		}
	}
	if skipPk >= 0 {
		quto = append(quto[:skipPk], quto[skipPk+1:]...)
		fields = append(fields[:skipPk], fields[skipPk+1:]...)
		args = append(args[:skipPk], args[skipPk+1:]...)
	}

	query := fmt.Sprintf(sqltpl, table, strings.Join(fields, ","), strings.Join(quto, ", "))
	if len(updatefields) > 0 {
		query += " on duplicate key update "
		for _, v := range updatefields {
			if strings.Contains(v, "=") {
				query += fmt.Sprintf("%s,", v)
			} else {
				query += fmt.Sprintf("`%s` = values(`%s`),", v, v)
			}
		}
		query = query[:len(query)-1]
	}
	return query, args, nil
}

func (s *Session) Delete() (int64, error) {
	if s.table == "" {
		return 0, errors.New("tablename faild")
	}
	str := bytes.NewBuffer(nil)
	str.WriteString(fmt.Sprintf("delete from %s", s.table))
	if s.indexs != nil {
		str.WriteString(fmt.Sprintf(" use index(%s)", strings.Join(s.indexs, ", ")))
	}
	condstr, condargs := s.cond.Build()
	str.WriteString(condstr)

	for _, v := range s.parts {
		str.WriteString(v.String())
	}
	return s.Exec(str.String(), condargs...)
}

func (s *Session) Update(obj interface{}) (int64, error) {
	v := reflect.Indirect(reflect.ValueOf(obj))
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	var (
		keys   []string
		values []interface{}
		str    = bytes.NewBuffer(nil)
		args   = make([]interface{}, 0)
		qort   = make([]string, 0)
	)
	switch v.Kind() {
	case reflect.Map:
		keys, values = parseMap(v)
	case reflect.Struct:
		keys, values = parseStruct(v)
	default:
		return 0, errorParseData
	}
	str.WriteString(fmt.Sprintf("update %s", s.table))
	if s.indexs != nil {
		str.WriteString(fmt.Sprintf(" use index(%s)", strings.Join(s.indexs, ", ")))
	}
	str.WriteString(" set")
	if v.Kind() == reflect.Struct {
		// 结构体默认是不更新空字段的
		// 如果要更新请指定cols
		if len(s.fields) > 0 {
			for _, a := range s.fields {
				for i, x := range keys {
					if x == a {
						qort = append(qort, fmt.Sprintf(" %s = ?", a))
						args = append(args, values[i])
						break
					}
				}
			}
		} else {
			for i, x := range values {
				rev := reflect.ValueOf(x)
				if rev.IsNil() || rev.IsZero() {
					continue
				}
				qort = append(qort, fmt.Sprintf(" %s = ?", keys[i]))
				args = append(args, values[i])
			}
		}
	} else {
		// map 更新所有 忽略cols
		for i, a := range keys {
			ex, ok := values[i].(Expression)
			if ok {
				qort = append(qort, fmt.Sprintf(" %s = %v", a, ex))
			} else {
				qort = append(qort, fmt.Sprintf(" %s = ?", a))
				args = append(args, values[i])
			}
		}
	}

	if len(qort) == 0 {
		return 0, errors.New("update field empty")
	}

	condstr, condargs := s.cond.Build()
	str.WriteString(strings.Join(qort, ", "))
	str.WriteString(condstr)

	return s.Exec(str.String(), append(args, condargs...)...)
}

func (s *Session) Tx(fn func(*Session) error) error {
	if s.tx != nil {
		return errors.New("alreay in tx")
	}
	tx, err := s.dao.DB().Begin()
	if err != nil {
		return err
	}
	s.tx = tx
	defer func() {
		s.tx = nil
	}()
	if err = fn(s); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
