package orm

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"time"
)

type structFieldInfo struct {
	name   string
	isjson bool
}

type rtCache struct {
	list map[reflect.Type]rtStructCache
	sync.RWMutex
}

func (m *rtCache) get(v reflect.Value) rtStructCache {
	t := v.Type()
	field := rtStructCache{}
	for i := 0; i < t.NumField(); i++ {
		var (
			fv = v.Field(i)
			ft = t.Field(i)
		)
		if fv.CanInterface() {
			tag := ft.Tag.Get("db")
			if tag == "-" {
				continue
			}
			if ft.Anonymous {
				nestField := m.get(fv)
				for k, v := range nestField {
					field[k] = v
				}
				continue
			}
			var parsejson bool
			if ts := strings.Split(tag, ","); len(ts) == 2 {
				tag = ts[0]
				if strings.TrimSpace(ts[1]) == "json" {
					parsejson = true
				}
			}
			if tag == "" {
				tag = lowUpperString(ft.Name)
			}
			if !parsejson {
				switch ft.Type.Kind() {
				case reflect.Struct:
					if !ft.Type.ConvertibleTo(reflect.TypeOf(time.Time{})) {
						parsejson = true
					}
				case reflect.Slice:
					// 排除[]byte
					if ft.Type.Elem().Kind() != reflect.Uint8 {
						parsejson = true
					}
				}
			}
			field[tag] = structFieldInfo{name: ft.Name, isjson: parsejson}
		}
	}
	return field
}

func (m *rtCache) Get(v reflect.Value) rtStructCache {
	t := v.Type()
	m.RLock()
	mp, ok := m.list[t]
	m.RUnlock()
	if ok {
		return mp
	}
	m.Lock()
	defer m.Unlock()
	field := m.get(v)
	m.list[t] = field
	return field
}

type rtStructCache map[string]structFieldInfo

func scanMap(v reflect.Value, cols []string, rows *sql.Rows) error {
	// map的具体类型
	args := make([]interface{}, len(cols))
	t := v.Type().Elem()
	var isMapString bool
	if t.Kind() == reflect.String {
		// 如果是map[string]string
		// 则需要转为[]byte,否则如果数据包含null则会报错 转成[]byte则null会转为空字符串
		isMapString = true
		t = reflect.TypeOf(sql.RawBytes{})
	}
	for i := range cols {
		args[i] = reflect.New(t).Interface()
	}
	err := rows.Scan(args...)
	if err != nil {
		return err
	}
	var arg reflect.Value
	for i, name := range cols {
		if isMapString {
			arg = reflect.ValueOf(string(*(args[i]).(*sql.RawBytes)))
		} else {
			arg = reflect.ValueOf(args[i]).Elem()
		}
		v.SetMapIndex(reflect.ValueOf(name), arg)
	}
	return nil
}

func scanStruct(v reflect.Value, cols []string, rows *sql.Rows) error {
	args := make([]interface{}, len(cols))
	fields := globalRtCache.Get(v)

	// tmpbuf 返回的字段数多于结构体的字段数是代替
	tmpbuf := sql.RawBytes{}
	// 用于存需要解析json的部分
	jsondata := map[string]int{}

	defer func() {
		for k, fi := range jsondata {
			if x := args[fi]; x != nil {
				_ = json.Unmarshal(*(x.(*[]byte)), v.FieldByName(k).Addr().Interface())
			}
		}
	}()
	for i, name := range cols {
		if fd, ok := fields[name]; ok {
			if fd.isjson {
				args[i] = &[]byte{}
				jsondata[fd.name] = i
			} else {
				args[i] = v.FieldByName(fd.name).Addr().Interface()
			}
		} else {
			args[i] = &tmpbuf
		}
	}
	return rows.Scan(args...)
}

func parseStruct(v reflect.Value) ([]string, []interface{}) {
	var (
		fields = globalRtCache.Get(v)
		names  []string
		args   []interface{}
	)
	for name, f := range fields {
		val := v.FieldByName(f.name).Interface()
		names = append(names, name)
		if f.isjson {
			b, _ := json.Marshal(val)
			args = append(args, string(b))
		} else {
			args = append(args, val)
		}
	}
	return names, args
}

func parseMap(v reflect.Value) ([]string, []interface{}) {
	var (
		names []string
		args  []interface{}
	)
	lt := v.MapRange()
	for lt.Next() {
		names = append(names, lt.Key().Interface().(string))
		val := lt.Value().Interface()
		switch e := val.(type) {
		case string:
		case int64, int32, int, uint, uint8, uint32, uint64, float32, float64:
		case []byte:
		case time.Time:
		default:
			b, _ := json.Marshal(e)
			args = append(args, string(b))
			continue
		}
		args = append(args, val)
	}
	return names, args
}
