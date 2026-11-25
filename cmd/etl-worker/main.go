package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/longkeyy/go-datax/internal/worker"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "监听地址")
	token := flag.String("token", "", "shared worker token (or ETL_WORKER_TOKEN)")
	flag.Parse()

	workerToken := *token
	if workerToken == "" {
		workerToken = os.Getenv("ETL_WORKER_TOKEN")
	}
	if workerToken == "" {
		log.Fatal("etl-worker: token is required (--token or ETL_WORKER_TOKEN)")
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("etl-worker: listen: %v", err)
	}

	server := worker.NewServer(workerToken)
	fmt.Printf("etl-worker listening on http://%s\n", ln.Addr().String())
	if err := http.Serve(ln, server.Handler()); err != nil {
		log.Fatalf("etl-worker: serve: %v", err)
	}
}
