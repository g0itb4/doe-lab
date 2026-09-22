package mem_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"doelab/api/internal/domain"
	"doelab/api/internal/repo/mem"
)

func TestObjects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o := mem.NewObjects()

	body := []byte("a,b\n1,2\n")
	if err := o.Put(ctx, "runs/r1.csv", "text/csv", body); err != nil {
		t.Fatal(err)
	}
	body[0] = 'X' // the store keeps its own copy
	got, err := o.Get(ctx, "runs/r1.csv")
	if err != nil || string(got) != "a,b\n1,2\n" {
		t.Errorf("get = %q, %v", got, err)
	}
	if obj, ok := o.Stat("runs/r1.csv"); !ok || obj.ContentType != "text/csv" {
		t.Errorf("stat = %+v, %v", obj, ok)
	}
	if _, ok := o.Stat("nope"); ok {
		t.Error("stat of a missing key")
	}
	if _, err := o.Get(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get a missing key: %v", err)
	}
	url, err := o.PresignGet(ctx, "runs/r1.csv", 15*time.Minute)
	if err != nil || !strings.Contains(url, "runs%2Fr1.csv") || !strings.Contains(url, "15m0s") {
		t.Errorf("presign = %q, %v", url, err)
	}
}
