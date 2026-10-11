package integration

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"travel-planner/travel-planner-api/internal/database"
)

func destinationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, url string) int64 {
	t.Helper()
	var id int64
	ref := "N" + strconv.FormatInt(time.Now().UnixNano(), 10)
	err := pool.QueryRow(ctx, `INSERT INTO places (provider_place_id,kind,name,address,latitude,longitude,time_zone,refreshed_at)
		VALUES ($1,'city','Cebu','Cebu, Philippines',10.3157,123.8854,'Asia/Manila',now()) RETURNING id`, ref).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p, err := database.Open(context.Background(), url)
		if err != nil {
			t.Error(err)
			return
		}
		defer p.Close()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := p.Exec(c, "DELETE FROM places WHERE id=$1", id); err != nil {
			t.Error(err)
		}
	})
	return id
}
