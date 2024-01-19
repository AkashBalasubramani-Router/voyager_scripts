package main

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/starknet.go/account"
	rpc "github.com/NethermindEth/starknet.go/rpc"
	"github.com/NethermindEth/starknet.go/utils"
	starknetutils "github.com/NethermindEth/starknet.go/utils"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
)

var (
	account_addr     string = "0x06f36e8a0fc06518125bbb1c63553e8a7d8597d437f9d56d891b8c7d3c977716"
	privateKey       string = "0x0687bf84896ee63f52d69e6de1b41492abeadc0dc3cb7bd351d0a52116915937"
	public_key       string = "0x58b0824ee8480133cad03533c8930eda6888b3c5170db2f6e4f51b519141963"
	INTEGRATION_BASE string = "https://starknet-testnet.public.blastapi.io"
	contractAddress  string = "0x01065cabc38e35848f47aab1ee9d5e28e279dfcf14bfc8968b81fe6095eacdc6"
	MaxNonceRetries         = 5
)

func invoke_iDeposit(partner_id *big.Int, dest_chain_id_bytes string, recipient []string, source_token string, amount *big.Int, dest_amount *big.Int, dest_token []string) (*rpc.AddInvokeTransactionResponse, error) {

	var calldata []*felt.Felt

	partner_id_felt := utils.BigIntToFelt(partner_id)
	partner_id_high, err := utils.HexToFelt("0x0")
	if err != nil {
		return nil, err
	}

	dest_chain_id_bytes_felt, err := utils.HexToFelt(dest_chain_id_bytes)
	if err != nil {
		return nil, err
	}

	recipient_felt, err := utils.HexArrToFelt(recipient)
	if err != nil {
		return nil, err
	}

	source_token_felt, err := utils.HexToFelt(source_token)
	if err != nil {
		return nil, err
	}

	amount_felt := utils.BigIntToFelt(amount)

	amount_high, err := utils.HexToFelt("0x0")
	if err != nil {
		return nil, err
	}

	dest_amount_felt := utils.BigIntToFelt(dest_amount)
	dest_amount_high, err := utils.HexToFelt("0x0")
	if err != nil {
		return nil, err
	}

	dest_token_felt, err := utils.HexArrToFelt(dest_token)
	if err != nil {
		return nil, err
	}

	calldata = append(calldata, partner_id_felt)
	calldata = append(calldata, partner_id_high)
	calldata = append(calldata, dest_chain_id_bytes_felt)
	calldata = generic_append(recipient_felt, calldata)
	calldata = append(calldata, source_token_felt)
	calldata = append(calldata, amount_felt)
	calldata = append(calldata, amount_high)
	calldata = append(calldata, dest_amount_felt)
	calldata = append(calldata, dest_amount_high)
	calldata = generic_append(dest_token_felt, calldata)

	c, err := ethrpc.DialContext(context.Background(), INTEGRATION_BASE)
	if err != nil {
		fmt.Println("Failed to connect to the client, did you specify the url in the .env.mainnet?")
		panic(err)
	}
	connection := rpc.NewProvider(c)
	account_address, err := starknetutils.HexToFelt(account_addr)
	if err != nil {
		panic(err.Error())
	}
	ks := account.NewMemKeystore()
	fakePrivKeyBI, ok := new(big.Int).SetString(privateKey, 0)
	if !ok {
		panic(err.Error())
	}
	ks.Put(public_key, fakePrivKeyBI)

	fmt.Println("Established connection with the client")

	account, err := account.NewAccount(connection, account_address, public_key, ks)

	maxfee, err := utils.HexToFelt("0x12f129f174ee")
	if err != nil {
		return nil, err
	}

	contract_address, err := utils.HexToFelt(contractAddress)
	if err != nil {
		return nil, err
	}

	nonce, err := account.Nonce(context.Background(), rpc.BlockID{Tag: "latest"}, account.AccountAddress)
	if err != nil {
		return nil, err
	}
	fmt.Println("Nonce 1--->: ", nonce)

	FnCall := rpc.FunctionCall{
		ContractAddress:    contract_address,
		EntryPointSelector: utils.GetSelectorFromNameFelt("iDeposit"),
		Calldata:           calldata,
	}

	var resp *rpc.AddInvokeTransactionResponse
	for i := 0; i < MaxNonceRetries; i++ {
		InvokeTx := rpc.InvokeTxnV1{
			MaxFee:        maxfee,
			Nonce:         nonce,
			Version:       rpc.TransactionV1,
			Type:          rpc.TransactionType_Invoke,
			SenderAddress: account.AccountAddress,
		}

		// CairoContractVersion specifies the version of Cairo being used
		CairoContractVersion := 2

		// Building the Calldata
		InvokeTx.Calldata, err = account.FmtCalldata([]rpc.FunctionCall{FnCall}, CairoContractVersion)
		if err != nil {
			return nil, err
		}

		fmt.Println("Invoke Tx Calldata: ", InvokeTx.Calldata)

		err = account.SignInvokeTransaction(context.Background(), &InvokeTx)
		if err != nil {
			return nil, err
		}

		fmt.Println(" INVOKING time  Nonce : ", nonce)

		resp, err = account.AddInvokeTransaction(context.Background(), InvokeTx)
		if err == nil {
			waitUntilMinedOrHandleError(account, resp.TransactionHash)
		}

		if err != nil {
			if strings.Contains(err.Error(), "A transaction with the same hash already exists in the mempool") || strings.Contains(err.Error(), "Invalid transaction nonce") {
				// Increment nonce to create a new unique transaction
				nonce = nonce.Add(nonce, new(felt.Felt).SetUint64(1))
				InvokeTx.Nonce = nonce
				fmt.Println("Incremented Nonce: ", nonce)
				// waitUntilMinedOrHandleError(c.account, resp.TransactionHash)
				continue // Retry with incremented nonce
			}
			return nil, err
		}
		break
	}

	if err != nil {
		return nil, fmt.Errorf("failed to submit transaction after %d retries: %w", MaxNonceRetries, err)
	}

	return resp, nil

}

func waitUntilMinedOrHandleError(c *account.Account, txhash *felt.Felt) {

	pollInterval := 5 * time.Second

	for {
		_, err := c.WaitForTransactionReceipt(context.Background(), txhash, pollInterval)
		if err == nil {

			return
		}
		fmt.Println("Waiting for transaction to be mined:", err)
		time.Sleep(pollInterval)
	}
}

func generic_append(val []*felt.Felt, to_append []*felt.Felt) []*felt.Felt {
	size := utils.Uint64ToFelt(uint64(len(val)))
	to_append = append(to_append, size)
	to_append = append(to_append, val...)
	return to_append
}

func main() {
	partner_id := big.NewInt(1023)
	dest_chain_id_bytes := "0x3830303031"
	recipient := []string{"0x5f04693482cfc121ff244cb3c3733af712f9df02"}
	source_token := "0x03c97f8274030ddec4edf1f0d28095edc3abd1fad122de15badec900019b677a"
	amount := big.NewInt(3000000)
	dest_amount := big.NewInt(2000000)
	dest_token := []string{}

	res, err := invoke_iDeposit(partner_id, dest_chain_id_bytes, recipient, source_token, amount, dest_amount, dest_token)
	if err != nil {
		fmt.Println("Error : ", err)
		return
	}

	fmt.Println("Transaction Hash : ", res.TransactionHash.String())

}
