package main

import (
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	_ "modernc.org/sqlite"
)

var GlobalDB *gorm.DB

func initDB() {
	db, err := gorm.Open(sqlite.New(sqlite.Config{
		DriverName: "sqlite",
		DSN:        filepath.Join(config.DataDir, "store/db/argus.db"),
	}), &gorm.Config{}) // 初始化数据库连接
	if err != nil {
		panic(err)
	}
	err = db.AutoMigrate(tables()...)
	if err != nil {
		panic(err)
	}
	GlobalDB = db
}
