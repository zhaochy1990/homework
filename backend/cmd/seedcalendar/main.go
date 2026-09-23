// Command seedcalendar 导入 research/005 的 2026 上学日历种子。
//
//	go run ./cmd/seedcalendar            # 导入为 verified=false（草稿）
//	go run ./cmd/seedcalendar -verified  # 人工核验后导入为 verified=true
package main

import (
	"context"
	"flag"
	"log"

	"github.com/zhaochy1990/homework/backend/internal/homework/calendar"
	"github.com/zhaochy1990/homework/backend/internal/homework/config"
	"github.com/zhaochy1990/homework/backend/internal/homework/database"
)

func main() {
	verified := flag.Bool("verified", false, "标记为已人工核验（参与业务判定）")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(cfg.DB, true)
	if err != nil {
		log.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		log.Fatal(err)
	}
	entries, err := calendar.Seed2026()
	if err != nil {
		log.Fatal(err)
	}
	n, err := calendar.NewStore(db).Import(context.Background(), entries, *verified)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("导入 %d 条上学日历（verified=%v）", n, *verified)
}
