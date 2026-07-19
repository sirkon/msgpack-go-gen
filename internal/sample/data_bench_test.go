package sample

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/sirkon/deepequal"
	"github.com/sirkon/msgpunsafe"
	"github.com/vmihailenco/msgpack/v5"
)

var dataSet []Data
var marshalDataSet [][]byte
var jsonDataSet [][]byte
var flatSet []Flat
var marshalFlatSet [][]byte
var jsonFlatSet [][]byte

func TestMain(m *testing.M) {
	dataSet = staticDataSet(0xFFFF)
	flatSet = staticFlatSet(0xFFFF)

	marshalDataSet = make([][]byte, 0, len(dataSet))
	for i, data := range dataSet {
		{
			res, err := msgpack.Marshal(data)
			if err != nil {
				panic(fmt.Errorf("marshal sample set data index %d: %w", i, err))
			}

			marshalDataSet = append(marshalDataSet, res)
		}

		{
			res, err := json.Marshal(data)
			if err != nil {
				panic(fmt.Errorf("marshal sample set data index %d into json: %w", i, err))
			}

			jsonDataSet = append(jsonDataSet, res)
		}
	}

	marshalFlatSet = make([][]byte, 0, len(dataSet))
	for i, flat := range flatSet {
		{
			res, err := msgpack.Marshal(flat)
			if err != nil {
				panic(fmt.Errorf("marshal sample set flat index %d: %w", i, err))
			}

			marshalFlatSet = append(marshalFlatSet, res)
		}

		{
			res, err := json.Marshal(flat)
			if err != nil {
				panic(fmt.Errorf("marshal sample set flat index %d: %w", i, err))
			}

			jsonFlatSet = append(jsonFlatSet, res)
		}
	}

	m.Run()
}

func BenchmarkMarshalData(b *testing.B) {
	b.Run("sirkon", func(b *testing.B) {
		b.Run("marshal", func(b *testing.B) {
			buf := make([]byte, 4096)
			for b.Loop() {
				buf = buf[:0]
				for i := range dataSet {
					_, err := dataSet[i].MarshalMsgpack(buf)
					if err != nil {
						b.Fatal(fmt.Errorf("marshal data index %d: %w", i, err))
					}
				}
			}
		})

		b.Run("unmarshal", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for i, packed := range marshalDataSet {
					var data Data
					buf := msgpunsafe.NewSafeBuffer(96)
					if err := data.UnmarshalMsgpack(packed, buf); err != nil {
						b.Fatal(fmt.Errorf("unmarshal data index %d: %w", i, err))
					}
				}
			}
		})

		b.Run("unmarshal-msgp", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for i, packed := range marshalDataSet {
					var data Data
					if _, err := data.UnmarshalMsg(packed); err != nil {
						b.Fatal(fmt.Errorf("unmarshal flat index %d with msgp: %w", i, err))
					}
				}
			}
		})
	})

	b.Run("vmihailenco", func(b *testing.B) {
		b.Run("marshal", func(b *testing.B) {
			for b.Loop() {
				for i := range dataSet {
					_, err := msgpack.Marshal(dataSet[i])
					if err != nil {
						b.Fatal(fmt.Errorf("marshal data index %d: %w", i, err))
					}
				}
			}
		})

		b.Run("unmarshal", func(b *testing.B) {
			for b.Loop() {
				for i, packed := range marshalDataSet {
					var data Data
					if err := msgpack.Unmarshal(packed, &data); err != nil {
						b.Fatal(fmt.Errorf("unmarshal data index %d: %w", i, err))
					}
				}
			}
		})
	})

	b.Run("json", func(b *testing.B) {
		b.Run("marshal", func(b *testing.B) {
			for b.Loop() {
				for i := range dataSet {
					_, err := json.Marshal(dataSet[i])
					if err != nil {
						b.Fatal(fmt.Errorf("marshal data index %d into json: %w", i, err))
					}
				}
			}
		})

		b.Run("unmarshal", func(b *testing.B) {
			for b.Loop() {
				for i, packed := range jsonDataSet {
					var data Data
					if err := json.Unmarshal(packed, &data); err != nil {
						b.Fatal(fmt.Errorf("unmarshal data index %d from json: %w", i, err))
					}
				}
			}
		})
	})
}

func BenchmarkMarshalFlat(b *testing.B) {

	b.Run("sirkon", func(b *testing.B) {
		b.Run("marshal", func(b *testing.B) {
			buf := make([]byte, 4096)
			for b.Loop() {
				buf = buf[:0]
				for i := range flatSet {
					_, err := flatSet[i].MarshalMsgpack(buf)
					if err != nil {
						b.Fatal(fmt.Errorf("marshal flat index %d: %w", i, err))
					}
				}
			}
		})

		b.Run("unmarshal", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for i, packed := range marshalFlatSet {
					var flat Flat
					buf := msgpunsafe.NewSafeBuffer(64)
					if err := flat.UnmarshalMsgpack(packed, buf); err != nil {
						b.Fatal(fmt.Errorf("unmarshal flat index %d: %w", i, err))
					}
				}
			}
		})

		b.Run("unmarshal-msgp", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for i, packed := range marshalFlatSet {
					var flat Flat
					if _, err := flat.UnmarshalMsg(packed); err != nil {
						b.Fatal(fmt.Errorf("unmarshal flat index %d with msgp: %w", i, err))
					}
				}
			}
		})
	})

	b.Run("vmihailenco", func(b *testing.B) {
		b.Run("marshal", func(b *testing.B) {
			for b.Loop() {
				for i := range flatSet {
					_, err := msgpack.Marshal(flatSet[i])
					if err != nil {
						b.Fatal(fmt.Errorf("marshal flat index %d: %w", i, err))
					}
				}
			}
		})

		b.Run("unmarshal", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for i, packed := range marshalFlatSet {
					var flat Flat
					if err := msgpack.Unmarshal(packed, &flat); err != nil {
						b.Fatal(fmt.Errorf("unmarshal flat index %d: %w", i, err))
					}
				}
			}
		})
	})

	b.Run("json", func(b *testing.B) {
		b.Run("marshal", func(b *testing.B) {
			for b.Loop() {
				for i := range flatSet {
					_, err := json.Marshal(flatSet[i])
					if err != nil {
						b.Fatal(fmt.Errorf("marshal flat index %d into json: %w", i, err))
					}
				}
			}
		})

		b.Run("unmarshal", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				for i, packed := range jsonFlatSet {
					var flat Flat
					if err := json.Unmarshal(packed, &flat); err != nil {
						b.Fatal(fmt.Errorf("unmarshal flat index %d from json: %w", i, err))
					}
				}
			}
		})
	})
}

func TestMakeSure(t *testing.T) {
	t.Run("sirkon undestands vmihailenco", func(t *testing.T) {
		for _, data := range dataSet {
			packed, err := msgpack.Marshal(data)
			if err != nil {
				t.Fatal(fmt.Errorf("pack with vmihailenco: %w", err))
			}

			buf := msgpunsafe.NewSafeBuffer(128)
			var got Data
			if err := got.UnmarshalMsgpack(packed, buf); err != nil {
				t.Fatal(fmt.Errorf("unmarshal vmihailenco packed data with sirkon unmarshaler: %w", err))
			}

			if !reflect.DeepEqual(&data, &got) {
				deepequal.SideBySide(t, "pack with vmihailenco and unpack with sirkon", &data, &got)
				return
			}
		}
	})

	t.Run("vmihailenco understands sirkon", func(t *testing.T) {
		for _, data := range dataSet {
			packed, err := data.MarshalMsgpack(nil)
			if err != nil {
				t.Fatal(fmt.Errorf("pack with sirkon: %w", err))
			}

			var got Data
			if err := msgpack.Unmarshal(packed, &got); err != nil {
				t.Fatal(fmt.Errorf("unmarshal sirkon packed data with vmihailenco unmarshaler: %w", err))
			}

			if !reflect.DeepEqual(&data, &got) {
				deepequal.SideBySide(t, "pack with sirkon and unpack with vmihailenco", &data, &got)
				return
			}
		}
	})
}

// randString генерирует строку заданной длины
func randString(n int) string {
	b := make([]byte, (n+1)/2)
	_, _ = cryptorand.Read(b)
	return hex.EncodeToString(b)[:n]
}

// staticDataSet выдает одинаковый набор данных благодаря фиксированному сиду PCG
func staticDataSet(n int) []Data {
	// Фиксированный сид гарантирует воспроизводимость датасета
	pcg := rand.NewPCG(42, 2026)
	r := rand.New(pcg)

	dataset := make([]Data, n)
	for i := 0; i < n; i++ {
		// Слайс Subs (0-10)
		subsLen := r.IntN(11)
		subs := make([]Sub, subsLen)
		for j := 0; j < subsLen; j++ {
			subs[j] = Sub{
				Key:    randString(r.IntN(10) + 5),
				Active: r.IntN(2) == 0,
			}
		}

		// Слайс Weights (0-15)
		weightsLen := r.IntN(16)
		weights := make([]uint64, weightsLen)
		for j := 0; j < weightsLen; j++ {
			weights[j] = r.Uint64()
		}

		// Мапа Meta (0-5)
		metaLen := r.IntN(6)
		meta := make(map[string]Sub, metaLen)
		for j := 0; j < metaLen; j++ {
			meta[randString(8)] = Sub{
				Key:    randString(r.IntN(10) + 5),
				Active: r.IntN(2) == 0,
			}
		}

		// Мапа Flags (0-5)
		flagsLen := r.IntN(6)
		flags := make(map[string]bool, flagsLen)
		for j := 0; j < flagsLen; j++ {
			flags[randString(6)] = r.IntN(2) == 0
		}

		dataset[i] = Data{
			Name:  randString(r.IntN(20) + 5),
			Count: r.IntN(100000),
			Subs:  subs,
			Internal: struct {
				Value float32 `msgpack:"value"`
			}{
				Value: r.Float32() * 100,
			},
			Weights: weights,
			Meta:    meta,
			Flags:   flags,
		}
	}

	return dataset
}

func randLimString(r *rand.Rand, n int) string {
	b := make([]byte, (n+1)/2)
	_, _ = cryptorand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func staticFlatSet(n int) []Flat {
	pcg := rand.NewPCG(100, 500)
	r := rand.New(pcg)

	dataset := make([]Flat, n)
	for i := 0; i < n; i++ {
		dataset[i] = Flat{
			Name:       randLimString(r, r.IntN(10)+5),
			Surname:    randLimString(r, r.IntN(15)+5),
			Patronymic: randLimString(r, r.IntN(15)+5),
			City:       randLimString(r, r.IntN(12)+4),
			Age:        r.IntN(100),
			Weight:     r.IntN(120) + 40,
			Fortune:    r.IntN(10000000),
		}
	}
	return dataset
}
