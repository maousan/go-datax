// Command genrows writes the deterministic comma-separated dataset used by the
// file -> SQLite benchmark scenario. Row n is a pure function of n, matching the
// SQL generator in bench/scripts/setup-mysql.sql.
//
// Usage (from the repository root):
//
//	go run ./bench/tools/genrows -rows 500000 -out bench/data/rows-500k.txt
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	rows := flag.Int("rows", 500000, "number of rows to generate")
	out := flag.String("out", "bench/data/rows-500k.txt", "output file path")
	flag.Parse()

	if *rows <= 0 {
		fmt.Fprintln(os.Stderr, "rows must be positive")
		os.Exit(2)
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create output dir: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create output: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	w := bufio.NewWriterSize(f, 1<<20)
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 1; i <= *rows; i++ {
		note := fmt.Sprintf("note-%d", i%1000)
		if i%7 == 0 {
			// \N is the DataX txtfilereader default nullFormat and is also
			// interpreted as SQL NULL by LOAD DATA.
			note = "\\N"
		}
		// id,name,email,age,score,status,city,payload,created_at,updated_at,flag,note
		_, err := fmt.Fprintf(w,
			"%d,user_%08d,user%08d@example.com,%d,%d.%02d,%s,%s,payload-%010d-%s,%s,%s,%d,%s\n",
			i, i, i,
			18+i%60,
			(i%10000)/100, i%100,
			statuses[i%4],
			cities[i%8],
			i, payloadSuffix,
			base.Add(time.Duration(i%86400)*time.Second).Format("2006-01-02 15:04:05"),
			base.Add(time.Duration((i*7)%86400)*time.Second).Format("2006-01-02 15:04:05"),
			i%2,
			note,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "write row %d: %v\n", i, err)
			os.Exit(1)
		}
	}

	if err := w.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "flush: %v\n", err)
		os.Exit(1)
	}

	info, err := f.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "stat: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d rows, %d bytes to %s\n", *rows, info.Size(), *out)
}

var statuses = [4]string{"active", "inactive", "pending", "deleted"}

var cities = [8]string{"Beijing", "Shanghai", "Guangzhou", "Shenzhen", "Hangzhou", "Chengdu", "Wuhan", "Xian"}

const payloadSuffix = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
