package insaneJSON

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"reflect"
	"testing"
	"unsafe"
)

type workload struct {
	json []byte
	name string

	requests [][]string
}

func getStableWorkload() ([]*workload, int64) {
	workloads := make([]*workload, 0, 0)
	workloads = append(workloads, loadJSON("light-ws", [][]string{
		{"_id"},
		{"favoriteFruit"},
		{"about"},
	}))
	workloads = append(workloads, loadJSON("many-objects", [][]string{
		{"deeper", "deeper", "deeper", "deeper", "deeper", "deeper", "deeper", "deeper", "deeper", "deeper", "deeper", "deeper", "deeper"},
	}))
	workloads = append(workloads, loadJSON("heavy", [][]string{
		{"first", "second", "third", "fourth", "fifth"},
	}))
	workloads = append(workloads, loadJSON("many-fields", [][]string{
		{"first"},
		{"middle"},
		{"last"},
	}))
	workloads = append(workloads, loadJSON("few-fields", [][]string{
		{"first"},
		{"middle"},
		{"last"},
	}))
	workloads = append(workloads, loadJSON("insane", [][]string{
		{"statuses", "2", "user", "entities", "url", "urls", "0", "expanded_url"},
		{"statuses", "36", "retweeted_status", "user", "profile", "sidebar", "fill", "color"},
		{"statuses", "75", "entities", "user_mentions", "0", "screen_name"},
		{"statuses", "99", "coordinates"},
	}))

	size := 0
	for _, workload := range workloads {
		size += len(workload.json)
	}

	return workloads, int64(size)
}

func loadJSON(name string, requests [][]string) *workload {
	content, err := ioutil.ReadFile(fmt.Sprintf("benchdata/%s.json", name))
	if err != nil {
		panic(err.Error())
	}

	return &workload{json: content, name: name, requests: requests}
}

func getChaoticWorkload() ([][]byte, [][][]string, int64) {
	lines := make([][]byte, 0, 0)
	requests := make([][][]string, 0, 0)
	file, err := os.Open("./benchdata/chaotic-workload.log")
	if err != nil {
		panic(err.Error())
	}
	defer func() {
		_ = file.Close()
	}()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		bytes := []byte(scanner.Text())
		lines = append(lines, bytes)
		root, err := DecodeBytes(bytes)
		if err != nil {
			panic(err.Error())
		}

		requestList := make([][]string, 0, 0)
		requestCount := rand.Int() % 3
		for x := 0; x < requestCount; x++ {
			node := root.Node
			selector := make([]string, 0, 0)
			for {
				if !node.IsObject() {
					break
				}

				fields := node.AsFields()
				name := fields[rand.Int()%len(fields)].AsString()
				selector = append(selector, string([]byte(name)))

				node = node.Dig(name)
			}
			requestList = append(requestList, selector)
		}
		requests = append(requests, requestList)

		Release(root)
	}
	if err := scanner.Err(); err != nil {
		panic(err.Error())
	}

	s, _ := file.Stat()
	return lines, requests, s.Size()
}

// BenchmarkFair benchmarks overall performance of libs as fair as it can:
// * using various JSON payload
// * decoding
// * doing low and high count of search requests
// * encoding
func BenchmarkFair(b *testing.B) {

	// some big buffer to avoid allocations
	s := make([]byte, 0, 512*1024)

	// let's make it deterministic as hell
	rand.Seed(666)

	// do little and few amount of search request
	requestsCount := []int{1, 8}

	pretenders := []struct {
		name string
		fn   func(b *testing.B, jsons [][]byte, fields [][][]string, reqCount int)
	}{
		{
			name: "complex",
			fn: func(b *testing.B, jsons [][]byte, fields [][][]string, reqCount int) {
				root := Spawn()
				for i := 0; i < b.N; i++ {
					for _, json := range jsons {
						_ = root.DecodeBytes(json)
						for j := 0; j < reqCount; j++ {
							for _, f := range fields {
								for _, ff := range f {
									root.Dig(ff...)
								}
							}
						}
						s = root.Encode(s[:0])
					}
				}
				Release(root)
			},
		},
		{
			name: "get",
			fn: func(b *testing.B, jsons [][]byte, fields [][][]string, reqCount int) {
				root := Spawn()
				for _, json := range jsons {
					_ = root.DecodeBytes(json)
					for j := 0; j < reqCount; j++ {
						for _, f := range fields {
							for _, ff := range f {
								for i := 0; i < b.N; i++ {
									root.Dig(ff...)
								}
							}
						}
					}
				}
				Release(root)
			},
		},
	}

	workload, stableSize := getStableWorkload()
	workloads, requests, chaoticSize := getChaoticWorkload()
	//
	b.Run("complex-stable-flavor|"+pretenders[0].name, func(b *testing.B) {
		b.SetBytes(stableSize * int64(len(requestsCount)))
		b.ResetTimer()
		for _, reqCount := range requestsCount {
			for _, w := range workload {
				pretenders[0].fn(b, [][]byte{w.json}, [][][]string{w.requests}, reqCount)
			}
		}
	})

	b.Run("complex-chaotic-flavor|"+pretenders[0].name, func(b *testing.B) {
		b.SetBytes(chaoticSize * int64(len(requestsCount)))
		b.ResetTimer()
		for _, reqCount := range requestsCount {
			pretenders[0].fn(b, workloads, requests, reqCount)
		}
	})

	b.Run("get-stable-flavor|"+pretenders[1].name, func(b *testing.B) {
		b.SetBytes(stableSize)
		b.ResetTimer()
		for _, w := range workload {
			pretenders[1].fn(b, [][]byte{w.json}, [][][]string{w.requests}, 1)
		}
	})

	b.Run("get-chaotic-flavor|"+pretenders[1].name, func(b *testing.B) {
		b.SetBytes(chaoticSize)
		b.ResetTimer()
		pretenders[1].fn(b, workloads, requests, 1)
	})
}

func BenchmarkValueDecodeInt(b *testing.B) {
	tests := []struct {
		s string
		n int64
	}{
		{s: "", n: 0},
		{s: " ", n: 0},
		{s: "xxx", n: 0},
		{s: "-xxx", n: 0},
		{s: "1xxx", n: 0},
		{s: "-", n: 0},
		{s: "111 ", n: 0},
		{s: "1-1", n: 0},
		{s: "s1", n: 0},
		{s: "0", n: 0},
		{s: "-0", n: 0},
		{s: "5", n: 5},
		{s: "-5", n: -5},
		{s: " 0", n: 0},
		{s: " 5", n: 0},
		{s: "333", n: 333},
		{s: "-333", n: -333},
		{s: "1111111111", n: 1111111111},
		{s: "987654321", n: 987654321},
		{s: "123456789", n: 123456789},
		{s: "9223372036854775807", n: 9223372036854775807},
		{s: "-9223372036854775807", n: -9223372036854775807},
		{s: "9999999999999999999", n: 0},
		{s: "99999999999999999999", n: 0},
		{s: "-9999999999999999999", n: 0},
		{s: "-99999999999999999999", n: 0},
	}

	for i := 0; i < b.N; i++ {
		for _, test := range tests {
			decodeInt64(test.s)
		}
	}
}

func BenchmarkValueEscapeString(b *testing.B) {
	tests := []struct {
		s string
	}{
		{s: `"""\\\\\"""\'\"				\\\""|"|"|"|\\'\dasd'		|"|\\\\'\\\|||\\'"`},
		{s: `sfsafwefqwueibfiquwbfiuqwebfiuqwbfiquwbfqiwbfoqiwuefb""""""""""""""""""""""""`},
		{s: `sfsafwefqwueibfiquwbfiuqwebfiuqwbfiquwbfqiwbfoqiwuefbxxxxxxxxxxxxxxxxxxxxxxx"`},
	}

	out := make([]byte, 0, 0)
	for i := 0; i < b.N; i++ {
		for _, test := range tests {
			out = escapeString(out[:0], test.s)
		}
	}
}

func BenchmarkNg1(b *testing.B) {
	content, err := ioutil.ReadFile("benchdata/insane.json")
	if err != nil {
		panic(err.Error())
	}

	b.SetBytes(int64(len(content)))
	x := 0
	for i := 0; i < b.N; i++ {
		for _, c := range content {
			if c == '{' || c == '}' || c == '[' || c == ']' || c == '"' || c == ',' {
				x++
			}
		}
	}

	fmt.Printf("\ncount: %d\n", x/b.N)
}

func BenchmarkNg2(b *testing.B) {
	content, err := ioutil.ReadFile("benchdata/insane.json")
	if err != nil {
		panic(err.Error())
	}

	b.SetBytes(int64(len(content)))
	x := 0
	m := make([]byte, 256)
	m['{'] = 1
	m['}'] = 1
	m['['] = 1
	m[']'] = 1
	m[','] = 1
	m['"'] = 1
	for i := 0; i < b.N; i++ {
		for i := 0; i < len(content); i += 8 {
			l := content[i : i+8]
			if m[l[0]]+m[l[1]]+m[l[2]]+m[l[3]]+m[l[4]]+m[l[5]]+m[l[6]]+m[l[7]] > 0 {
				if m[l[0]] == 1 {
					x++
				}
				if m[l[1]] == 1 {
					x++
				}
				if m[l[2]] == 1 {
					x++
				}
				if m[l[3]] == 1 {
					x++
				}
				if m[l[4]] == 1 {
					x++
				}
				if m[l[5]] == 1 {
					x++
				}
				if m[l[6]] == 1 {
					x++
				}
				if m[l[7]] == 1 {
					x++
				}
			}
		}
	}

	fmt.Printf("\ncount: %d\n", x/b.N)
}

func BenchmarkNg3(b *testing.B) {
	content, err := ioutil.ReadFile("benchdata/insane.json")
	if err != nil {
		panic(err.Error())
	}

	b.SetBytes(int64(len(content)))
	x := 0
	root := Spawn()
	for i := 0; i < b.N; i++ {
		_ = root.DecodeBytes(content)
	}

	fmt.Printf("\ncount: %d\n", x/b.N)
}

func BenchmarkNg4(b *testing.B) {
	content, err := ioutil.ReadFile("benchdata/insane.json")
	if err != nil {
		panic(err.Error())
	}

	header := (*reflect.SliceHeader)(unsafe.Pointer(&content))

	ccc := content
	header2 := (*reflect.SliceHeader)(unsafe.Pointer(&ccc))
	b.SetBytes(int64(len(content)))
	x := 0
	flows := []byte(`{}[],`)
	for i := 0; i < b.N; i++ {
		for _, flow := range flows {
			header2.Len = header.Len
			header2.Data = header.Data
			header2.Cap = header.Cap
			data := &header2.Data
			l := &header2.Len
			for {
				pos := bytes.IndexByte(*(*[]byte)(unsafe.Pointer(header2)), flow) + 1
				if pos <= 0 {
					fmt.Printf("pos: %d\n", pos)
					break
				}
				x++
				*data += uintptr(pos)
				*l -= pos
			}
		}
	}

	fmt.Printf("\ncount: %d\n", x/b.N)
}

func BenchmarkNg5(b *testing.B) {
	r1, r2 := InsaneSkipWC_([]byte(`                         .`), []byte(`........`))
	fmt.Printf("res=%d/%d\n", r1%256, r2%256)
}
