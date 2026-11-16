package messaging

import (
	"context"
	"net/http"
	"os"
	"sync"
	"testing"
)

func TestClosedAndCancelledTransportDoesNotConnect(t *testing.T) {
	transport := &Transport{URL: "amqp://unused.invalid"}
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.outbound(); err == nil {
		t.Fatal("closed transport connected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := transport.call(ctx, "identity", Request{}); err != context.Canceled {
		t.Fatal("cancelled request was not stopped")
	}
}

func TestRpcConnectionReuse(t *testing.T) {
	address := os.Getenv("HYDROSMART_RPC_TEST_URL")
	if address == "" {
		t.Skip("set HYDROSMART_RPC_TEST_URL to run against an isolated broker")
	}
	transport := &Transport{URL: address}
	defer transport.Close()
	client := http.Client{Transport: transport}
	check := func(path string, want int) bool {
		response, err := client.Get("http://config-service" + path)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == want
	}
	if !check("/health/live", 200) {
		t.Fatal("initial RPC failed")
	}
	original := transport.connection
	if !check("/health/live", 200) || transport.connection != original {
		t.Fatal("connection was not reused")
	}
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			path, want := "/health/live", 200
			if i%2 != 0 {
				path, want = "/rpc-missing-probe", 404
			}
			if !check(path, want) {
				t.Error("concurrent replies crossed or failed")
			}
		}(i)
	}
	group.Wait()
	if transport.connection != original {
		t.Fatal("concurrent requests opened more connections")
	}
	original.Close()
	if !check("/health/live", 200) || transport.connection == original {
		t.Fatal("closed connection was not replaced")
	}
}
