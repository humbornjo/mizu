package fieldmaskx_test

import (
	"testing"

	"github.com/humbornjo/mizu/x/fieldmaskx"
)

var (
	sinkMask any
	sinkErr  error
)

type benchSimple struct {
	Name   string  `json:"name"`
	Age    int     `json:"age"`
	Email  string  `json:"email"`
	Active bool    `json:"active"`
	Score  float64 `json:"score"`
}

type benchMoney struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

func (benchMoney) MarshalJSON() ([]byte, error) {
	return []byte(`"0"`), nil
}

type benchTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type benchGeo struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type benchAddress struct {
	Street string     `json:"street"`
	City   string     `json:"city"`
	Geo    *benchGeo  `json:"geo"`
	Tags   []benchTag `json:"tags"`
}

type benchCustomer struct {
	Name    string        `json:"name"`
	Address *benchAddress `json:"address"`
}

type benchItem struct {
	Sku     string     `json:"sku"`
	Qty     int        `json:"qty"`
	Price   benchMoney `json:"price"`
	Options []benchTag `json:"options"`
}

type benchEvent struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
}

type benchOrderBase struct {
	Id        string `json:"id"`
	CreatedAt string `json:"createdAt"`
}

type benchOrder struct {
	benchOrderBase
	Customer  benchCustomer            `json:"customer"`
	Items     []benchItem              `json:"items"`
	History   [4]benchEvent            `json:"history"`
	Labels    map[string]string        `json:"labels"`
	Shipments map[string]*benchAddress `json:"shipments"`
	Total     benchMoney               `json:"total"`
	Note      *string                  `json:"note"`
	Secret    string                   `json:"-"`
	internal  int
}

var benchSimplePaths = []string{"name", "age", "email"}

var benchOrderPaths = []string{
	"id", "createdAt",
	"customer.name", "customer.address.city", "customer.address.geo.lat",
	"customer.address.tags.key",
	"items.sku", "items.qty", "items.price", "items.options.value",
	"history.kind",
	"labels",
	"shipments.primary.city",
	"total",
	"note",
}

func newBenchSimple() benchSimple {
	return benchSimple{
		Name: "ada", Age: 36, Email: "ada@example.com", Active: true, Score: 9.5,
	}
}

func newBenchOrder() benchOrder {
	note := "leave at door"
	return benchOrder{
		benchOrderBase: benchOrderBase{Id: "order-1", CreatedAt: "2026-09-14T00:00:00Z"},
		Customer: benchCustomer{
			Name: "ada",
			Address: &benchAddress{
				Street: "1 main st", City: "shanghai",
				Geo:  &benchGeo{Lat: 31.2, Lng: 121.5},
				Tags: []benchTag{{Key: "kind", Value: "home"}, {Key: "floor", Value: "3"}},
			},
		},
		Items: []benchItem{
			{
				Sku: "sku-1", Qty: 2, Price: benchMoney{Amount: 199, Currency: "USD"},
				Options: []benchTag{{Key: "color", Value: "red"}},
			},
			{Sku: "sku-2", Qty: 1, Price: benchMoney{Amount: 599, Currency: "USD"}},
		},
		History: [4]benchEvent{
			{At: "t0", Kind: "created"}, {At: "t1", Kind: "paid"},
			{At: "t2", Kind: "packed"}, {At: "t3", Kind: "shipped"},
		},
		Labels: map[string]string{"channel": "web", "priority": "high"},
		Shipments: map[string]*benchAddress{
			"primary": {Street: "1 main st", City: "shanghai", Geo: &benchGeo{Lat: 31.2, Lng: 121.5}},
			"backup":  {Street: "2 side st", City: "beijing"},
		},
		Total:    benchMoney{Amount: 997, Currency: "USD"},
		Note:     &note,
		Secret:   "secret",
		internal: 1,
	}
}

func BenchmarkFieldMaskx_Intersect(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkMask = fieldmaskx.Intersect[benchSimple](benchSimplePaths, benchSimplePaths)
		}
	})
	b.Run("complicated", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkMask = fieldmaskx.Intersect[benchOrder](benchOrderPaths, benchOrderPaths)
		}
	})
}

func BenchmarkFieldMaskx_Filter(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		mask := fieldmaskx.Intersect[benchSimple](benchSimplePaths, benchSimplePaths)
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			value := newBenchSimple()
			b.StartTimer()
			sinkErr = mask.Filter(&value)
		}
	})
	b.Run("complicated", func(b *testing.B) {
		mask := fieldmaskx.Intersect[benchOrder](benchOrderPaths, benchOrderPaths)
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			value := newBenchOrder()
			b.StartTimer()
			sinkErr = mask.Filter(&value)
		}
	})
}

func BenchmarkFieldMaskx_Prune(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		mask := fieldmaskx.Intersect[benchSimple](benchSimplePaths, benchSimplePaths)
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			value := newBenchSimple()
			b.StartTimer()
			sinkErr = mask.Prune(&value)
		}
	})
	b.Run("complicated", func(b *testing.B) {
		mask := fieldmaskx.Intersect[benchOrder](benchOrderPaths, benchOrderPaths)
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			value := newBenchOrder()
			b.StartTimer()
			sinkErr = mask.Prune(&value)
		}
	})
}

func BenchmarkFieldMaskx_Overwrite(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		mask := fieldmaskx.Intersect[benchSimple](benchSimplePaths, benchSimplePaths)
		source := newBenchSimple()
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			target := newBenchSimple()
			b.StartTimer()
			sinkErr = mask.Overwrite(&source, &target)
		}
	})
	b.Run("complicated", func(b *testing.B) {
		mask := fieldmaskx.Intersect[benchOrder](benchOrderPaths, benchOrderPaths)
		source := newBenchOrder()
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			target := newBenchOrder()
			b.StartTimer()
			sinkErr = mask.Overwrite(&source, &target)
		}
	})
}
