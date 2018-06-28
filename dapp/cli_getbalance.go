package main

import (
	"fmt"
	"log"

	"github.com/dappworks/go-dappworks/core"
	"github.com/dappworks/go-dappworks/util"
)

func (cli *CLI) getBalance(address string) {
	if !core.ValidateAddress(address) {
		log.Panic("ERROR: Address is not valid")
	}
	bc := core.NewBlockchain(address)
	defer bc.DB.Close()

	balance := 0
	pubKeyHash := util.Base58Decode([]byte(address))
	pubKeyHash = pubKeyHash[1 : len(pubKeyHash)-4]
	UTXOs := bc.FindUTXO(pubKeyHash)

	for _, out := range UTXOs {
		balance += out.Value
	}

	fmt.Printf("Balance of '%s': %d\n", address, balance)
}
