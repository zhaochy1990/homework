package model

import "testing"

func TestTableNames(t *testing.T) {
	want := map[string]string{
		User{}.TableName():             "users",
		Child{}.TableName():            "children",
		Guardianship{}.TableName():     "guardianships",
		Class{}.TableName():            "classes",
		ClassJoinRequest{}.TableName(): "class_join_requests",
		ClassMember{}.TableName():      "class_members",
		ChildEnrollment{}.TableName():  "child_enrollments",
		Textbook{}.TableName():         "textbooks",
		TextbookUnit{}.TableName():     "textbook_units",
		ClassTextbook{}.TableName():    "class_textbooks",
		Material{}.TableName():         "materials",
		HomeworkSession{}.TableName():  "homework_sessions",
		Todo{}.TableName():             "todos",
		TodoTick{}.TableName():         "todo_ticks",
		Checkin{}.TableName():          "checkins",
		CheckinMedia{}.TableName():     "checkin_media",
		CheckinCard{}.TableName():      "checkin_cards",
		SchoolCalendar{}.TableName():   "school_calendar",
		NoHomeworkDay{}.TableName():    "no_homework_days",
		Streak{}.TableName():           "streaks",
	}
	for got, expected := range want {
		if got != expected {
			t.Errorf("table name = %q, want %q", got, expected)
		}
	}
}

func TestAllCoversEveryTable(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range All() {
		name := tableName(m)
		if seen[name] {
			t.Fatalf("duplicate table in All(): %s", name)
		}
		seen[name] = true
	}
	if len(seen) != 20 {
		t.Fatalf("All() has %d tables, want 20", len(seen))
	}
}

type tabler interface{ TableName() string }

func tableName(v any) string { return v.(tabler).TableName() }
