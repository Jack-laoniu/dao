package orm

import (
	"database/sql"
	"errors"
	"reflect"

	"github.com/afocus/trace"
)

var (
	errorRowsData  = errors.New("data should be a slice{map/struct}'s pointer")
	errorRowData   = errors.New("data should be a map/struct's pointer")
	errorParseData = errors.New("data should be map/struct")
	globalRtCache  = &rtCache{list: make(map[reflect.Type]rtStructCache)}
)

// QueryContext 负责对rows进行转换
type QueryContext struct {
	rows    *sql.Rows
	lastErr error
	tr      *trace.Trace
	rowCnt  int
}

func CreateQueryContext(rows *sql.Rows, err error) *QueryContext {
	return &QueryContext{rows: rows, lastErr: err}
}

// Get 转换单行数据
// 如果无结果 则返回sql.ErrNoRows
func (c *QueryContext) Get(obj interface{}) (bool, error) {
	if c.lastErr != nil {
		return false, c.lastErr
	}

	defer c.rows.Close()
	cols, err := c.rows.Columns()
	if err != nil {
		return false, err
	}

	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Ptr {
		return false, errorRowData
	}

	for !c.rows.Next() {
		return false, nil
	}

	v = reflect.Indirect(v)
	c.rowCnt += 1
	switch v.Kind() {
	case reflect.Map:
		return true, scanMap(v, cols, c.rows)
	case reflect.Struct:
		return true, scanStruct(v, cols, c.rows)
	default:
		return true, errorRowData
	}
}

// Find 转换多行数据
// obj 必须是slice类型 如[]struct []map[string]interface{}..
func (c *QueryContext) Find(obj interface{}) error {
	if c.lastErr != nil {
		return c.lastErr
	}
	defer c.rows.Close()
	cols, err := c.rows.Columns()
	if err != nil {
		return err
	}
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Slice {
		return errorRowsData
	}
	v = reflect.Indirect(v)
	// 列表项
	vitem := v.Type().Elem()

	var onePtr bool
	if vitem.Kind() == reflect.Ptr {
		onePtr = true
		vitem = vitem.Elem()
	}

	var one reflect.Value

	for c.rows.Next() {
		switch vitem.Kind() {
		case reflect.Map:
			one = reflect.MakeMap(vitem)
			if err := scanMap(one, cols, c.rows); err != nil {
				return err
			}
		case reflect.Struct:
			one = reflect.New(vitem).Elem()
			if err := scanStruct(one, cols, c.rows); err != nil {
				return err
			}
		default:
			return errorRowsData
		}
		if onePtr {
			v.Set(reflect.Append(v, one.Addr()))
		} else {
			v.Set(reflect.Append(v, one))
		}
		c.rowCnt++
	}
	return nil
}
