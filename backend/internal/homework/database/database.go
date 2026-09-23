// Package database 负责建立 GORM 连接并迁移全部表。
package database

import (
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/model"
)

// Open 连接 MySQL。dev=true 时打印慢查询与错误，生产只打错误。
func Open(cfg config.DB, dev bool) (*gorm.DB, error) {
	lvl := logger.Error
	if dev {
		lvl = logger.Warn
	}
	return gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		Logger:         logger.Default.LogMode(lvl),
		TranslateError: true,
	})
}

// Migrate 建出 data-model 定义的全部表。
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(model.All()...)
}
