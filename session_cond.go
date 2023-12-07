package orm

import (
	"fmt"
	"reflect"
	"strings"
)

type sessionCond struct {
	strb *strings.Builder
	args []interface{}
}

func newSessionCond(query string, args ...interface{}) *sessionCond {
	s := &sessionCond{
		strb: &strings.Builder{},
		args: make([]interface{}, 0),
	}
	s.addPart("where", query, args...)
	return s
}

func (s *sessionCond) And(query string, args ...interface{}) {
	s.addPart("and", query, args...)
}

func (s *sessionCond) Or(query string, args ...interface{}) {
	s.addPart("or", query, args...)
}

func (s *sessionCond) addPart(cmd string, query string, args ...interface{}) {
	query, args = placeholderExpansion(query, args)
	s.strb.WriteString(fmt.Sprintf(" %s ", cmd))
	s.strb.WriteString(fmt.Sprintf("( %s )", query))
	s.args = append(s.args, args...)
}

func (s *sessionCond) Build() (string, []interface{}) {
	return s.strb.String(), s.args
}

func parseSQLArgsInSlice(count int) string {
	s := strings.Split(strings.Repeat("?", count), "")
	return strings.Join(s, ",")
}

// placeholderExpansion 占位符展开
// where name = ? and id in (?)  ["afocus", []{1,3,4,5}] 展开为 where name = ? and id in (?,?,?,?)  ["afocus", 1,3,4,5]
func placeholderExpansion(query string, args []interface{}) (string, []interface{}) {
	var (
		values []interface{}
		ns     []byte
		n      int
	)
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			if query[i-1] == '(' {
				tv := reflect.ValueOf(args[n])
				x := tv.Type()
				if x.Kind() == reflect.Slice {
					length := tv.Len()
					ns = append(ns, parseSQLArgsInSlice(length)...)
					for j := 0; j < length; j++ {
						values = append(values, tv.Index(j).Interface())
					}
				} else {
					ns = append(ns, '?')
					values = append(values, tv.Interface())
				}
				n++
				continue
			}
			values = append(values, args[n])
			n++
		}
		ns = append(ns, query[i])
	}
	return string(ns), values
}
