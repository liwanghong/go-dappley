package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"github.com/dappworks/go-dappworks/logic"
)

// CLI responsible for processing command line arguments
type CLI struct{}

func (cli *CLI) printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  createblockchain -address ADDRESS")
	fmt.Println("  createwallet")
	fmt.Println("  getbalance -address ADDRESS")
	fmt.Println("  listaddresses")
	fmt.Println("  printchain")
	fmt.Println("  send -from FROM -to TO -amount AMOUNT")
}

func (cli *CLI) validateArgs() {
	if len(os.Args) < 2 {
		cli.printUsage()
		os.Exit(1)
	}
}

// Run parses command line arguments and processes commands
func (cli *CLI) Run() {
	cli.validateArgs()

	getBalanceCmd := flag.NewFlagSet("getbalance", flag.ExitOnError)
	createBlockchainCmd := flag.NewFlagSet("createblockchain", flag.ExitOnError)
	createWalletCmd := flag.NewFlagSet("createwallet", flag.ExitOnError)
	listAddressesCmd := flag.NewFlagSet("listaddresses", flag.ExitOnError)
	sendCmd := flag.NewFlagSet("send", flag.ExitOnError)
	printChainCmd := flag.NewFlagSet("printchain", flag.ExitOnError)

	getBalanceAddress := getBalanceCmd.String("address", "", "The address to get balance for")
	createBlockchainAddress := createBlockchainCmd.String("address", "", "The address to send genesis block reward to")
	sendFrom := sendCmd.String("from", "", "Source client address")
	sendTo := sendCmd.String("to", "", "Destination client address")
	sendAmount := sendCmd.Int("amount", 0, "Amount to send")

	switch os.Args[1] {
	case "getbalance":
		err := getBalanceCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
	case "createblockchain":
		err := createBlockchainCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
	case "createwallet":
		err := createWalletCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
	case "listaddresses":
		err := listAddressesCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
	case "printchain":
		err := printChainCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
	case "send":
		err := sendCmd.Parse(os.Args[2:])
		if err != nil {
			log.Panic(err)
		}
	default:
		cli.printUsage()
		os.Exit(1)
	}

	if getBalanceCmd.Parsed() {
		if *getBalanceAddress == "" {
			getBalanceCmd.Usage()
			os.Exit(1)
		}
		balance, err := logic.GetBalance(*getBalanceAddress)

		if err != nil{
			log.Println(err)
		}

		fmt.Printf("Balance of '%s': %d\n", *getBalanceAddress, balance)

	}

	if createBlockchainCmd.Parsed() {
		if *createBlockchainAddress == "" {
			createBlockchainCmd.Usage()
			os.Exit(1)
		}
		_, err := logic.CreateBlockchain(*createBlockchainAddress)
		if err != nil {
			log.Println(err)
		}else{
			fmt.Println("Create Blockchain Successful")
		}
	}

	if createWalletCmd.Parsed() {
		walletAddr, err := logic.CreateWallet()
		if err != nil {
			log.Println(err)
		}
		fmt.Printf("Your new address: %s\n", walletAddr)
	}

	if listAddressesCmd.Parsed() {
		addrs, err := logic.GetAllAddresses()
		if err != nil {
			log.Println(err)
		}
		for _, address := range addrs {
			fmt.Println(address)
		}
	}

	if printChainCmd.Parsed() {
		cli.printChain()
	}

	if sendCmd.Parsed() {
		if *sendFrom == "" || *sendTo == "" || *sendAmount <= 0 {
			sendCmd.Usage()
			os.Exit(1)
		}

		if err := logic.Send(*sendFrom, *sendTo, *sendAmount); err != nil{
			log.Println(err)
		}else{
			fmt.Println("Send Successful")
		}

	}
}
