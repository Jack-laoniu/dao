package orm

import (
	"context"
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
)

// type Logoutputer func(s string)

type Dao struct {
	database *sql.DB
	// logger   Logoutputer
}

func Create(dsn string) (*Dao, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	return &Dao{database: db}, nil
}

// func (dao *Dao) SetLogger(logger Logoutputer) {
// 	dao.logger = logger
// }

func (dao *Dao) NewSession(ctx context.Context) *Session {
	s := sessions.Get().(*Session)
	s.dao = dao
	s.ctx = ctx
	return s
}

// func (dao *Dao) NewSession(uniq ...string) *Session {
// 	s := sessions.Get().(*Session)
// 	if len(uniq) > 0 {
// 		s.uniq = uniq[0] + " "
// 	}
// 	s.dao = dao
// 	return s
// }

func (dao *Dao) DB() *sql.DB {
	return dao.database
}
