package utils

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"arcaeabot/internal/database"
)

func ExistsRow(ctx context.Context, db database.Queryer, query string, args ...any) bool {
	var one int
	return db.QueryRowContext(ctx, query, args...).Scan(&one) == nil
}

func Affected(res sql.Result) int64 {
	n, _ := res.RowsAffected()
	return n
}

func QQEmail(qq int64) string {
	return strconv.FormatInt(qq, 10) + "@qq.com"
}

func EmailQQ(email string) (int64, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.HasSuffix(email, "@qq.com") {
		return 0, false
	}
	qq, err := strconv.ParseInt(strings.TrimSuffix(email, "@qq.com"), 10, 64)
	return qq, err == nil && qq > 0
}

func UserID(ctx context.Context, db database.Queryer, qq int64) (int64, error) {
	var userID int64
	err := db.QueryRowContext(ctx, "SELECT user_id FROM user WHERE LOWER(TRIM(email))=?", QQEmail(qq)).Scan(&userID)
	return userID, err
}
