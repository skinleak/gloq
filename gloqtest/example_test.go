package gloqtest_test

import (
	"fmt"
	"log/slog"

	"github.com/skinleak/gloq/gloqtest"
)

func ExampleRecorder() {
	rec := gloqtest.NewRecorder()
	log := rec.Logger().With("service", "billing")

	log.Error("payment failed", "order_id", 42, slog.Group("card", "brand", "visa"))

	// In a test: rec.AssertLogged(t, slog.LevelError, "payment failed", "order_id", 42)
	record, ok := rec.Find(slog.LevelError, "payment failed", "order_id", 42)
	fmt.Println(ok, record)
	brand, _ := record.Value("card.brand")
	fmt.Println(brand)
	// Output:
	// true ERROR "payment failed" service="billing" order_id=42 card.brand="visa"
	// visa
}
