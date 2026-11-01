// Run with PIKVM_URL, PIKVM_USER, and PIKVM_PASSWORD set.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/infoe-labs/pikvm-cli/lib/pikvm"
)

func main() {
	client, err := pikvm.New(pikvm.Config{URL: os.Getenv("PIKVM_URL"), Username: os.Getenv("PIKVM_USER"), Password: os.Getenv("PIKVM_PASSWORD")})
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	state, err := client.ATX().State(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(state))
}
