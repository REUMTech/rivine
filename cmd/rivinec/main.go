package main

import (
	"github.com/rivine/rivine/pkg/client"
	"github.com/rivine/rivine/types"
)

func main() {
	// The name defaults to rivine if it isn't specified but set it again to make sure
	client.ClientName = "rivine"
	client.DefaultClient(types.NewCurrency64(1000000000000).Mul64(1000000000000))
}
