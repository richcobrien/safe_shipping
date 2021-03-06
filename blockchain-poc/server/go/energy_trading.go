package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/hyperledger/fabric/core/chaincode/shim"
)

var logger = shim.NewLogger("energy_trading")

const (
	tableName = "Meters"
)

type MeterInfo struct {
	Id             string  `json:"id"`
	Name           string  `json:"name"`
	Kwh            int64   `json:"kwh"`
	AccountBalance float64 `json:"account_balance"`
	RatePerKwh     int64   `json:"rate_per_kwh"`
}

type ByRate []*MeterInfo

func (a ByRate) Len() int {
	return len(a)
}

func (a ByRate) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

func (a ByRate) Less(i, j int) bool {
	return a[i].RatePerKwh < a[j].RatePerKwh
}

// EnergyTradingChainCode implementation. This smart contract enables multiple smart meters
// to enroll and report their production/consumption of energy. It then lets user settle
// their accounts by moving funds from consumers to producers.
type EnergyTradingChainCode struct {
}

func (t *EnergyTradingChainCode) Init(stub *shim.ChaincodeStub, function string, args []string) ([]byte, error) {
	var err error
	var val float64

	if len(args) == 0 {
		logger.Error("Incorrect number of arguments")
		return nil, errors.New("Incorrect number of arguments. Specify the exchange rate for this smart contract.")
	}

	val, err = strconv.ParseFloat(string(args[0]), 64)
	if err != nil {
		logger.Errorf("Invalid value %s for exchange rate", args[0])
		return nil, errors.New("Invalid value for exchange rate")
	}

	err = stub.PutState("exchange_rate", []byte(strconv.FormatFloat(val, 'f', 6, 64)))
	if err != nil {
		logger.Errorf("Error saving exchange rate %s", err.Error())
		return nil, errors.New("Exchange rate cannot be saved")
	}

	var exchangeAccountBalance float64
	exchangeAccountBalance = 0.0
	err = stub.PutState("exchange_account_balance", []byte(strconv.FormatFloat(exchangeAccountBalance, 'f', 6, 64)))
	if err != nil {
		logger.Errorf("Error saving exchange account balance %s", err.Error())
		return nil, errors.New("Exchange account balance cannot be saved")
	}

	_, err = stub.GetTable(tableName)
	if err == shim.ErrTableNotFound {
		err = stub.CreateTable(tableName, []*shim.ColumnDefinition{
			&shim.ColumnDefinition{Name: "AccountId", Type: shim.ColumnDefinition_STRING, Key: true},
			&shim.ColumnDefinition{Name: "AccountName", Type: shim.ColumnDefinition_STRING, Key: false},
			&shim.ColumnDefinition{Name: "ReportedKWH", Type: shim.ColumnDefinition_INT64, Key: false},
			&shim.ColumnDefinition{Name: "AccountBalance", Type: shim.ColumnDefinition_STRING, Key: false},
			&shim.ColumnDefinition{Name: "RatePerKWH", Type: shim.ColumnDefinition_INT64, Key: false},
		})
		if err != nil {
			logger.Errorf("Error creating table:%s", err.Error())
			return nil, errors.New("Failed creating AssetsOnwership table.")
		}
	} else {
		logger.Info("Table already exists")
	}

	logger.Info("Successfully deployed chain code")

	return nil, nil
}

func (t *EnergyTradingChainCode) Invoke(stub *shim.ChaincodeStub, function string, args []string) ([]byte, error) {

	if function == "enroll" {
		return t.enroll(stub, args)
	}

	if function == "delete" {
		return t.delete(stub, args)
	}

	if function == "changeAccountBalance" {
		return t.changeAccountBalance(stub, args)
	}

	if function == "reportDelta" {
		return t.reportDelta(stub, args)
	}

	if function == "settle" {
		return t.settle(stub, args)
	}

	logger.Errorf("Unimplemented method :%s called", function)

	return nil, errors.New("Unimplemented '" + function + "' invoked")
}

// Enrolls a new meter
func (t *EnergyTradingChainCode) enroll(stub *shim.ChaincodeStub, args []string) ([]byte, error) {
	logger.Info("In enroll function")
	if len(args) < 3 {
		logger.Error("Incorrect number of arguments")
		return nil, errors.New("Incorrect number of arguments. Specify account number, name and rate per kwh")
	}

	accountId := args[0]
	accountName := args[1]
	rateKwhStr := args[2]
	rateKwh, err := strconv.ParseInt(string(rateKwhStr), 10, 64)
	if err != nil {
		logger.Errorf("Error in converting to int:%s", err.Error())
		return nil, fmt.Errorf("Invalid value of rate per kwh:%s", rateKwhStr)
	}

	logger.Infof("Enrolling meter with id:%s, name:%s and target rate:%d", accountId, accountName, rateKwh)

	ok, err := stub.InsertRow(tableName, shim.Row{
		Columns: []*shim.Column{
			&shim.Column{Value: &shim.Column_String_{String_: accountId}},
			&shim.Column{Value: &shim.Column_String_{String_: accountName}},
			&shim.Column{Value: &shim.Column_Int64{Int64: 0}},
			&shim.Column{Value: &shim.Column_String_{String_: "0.0"}},
			&shim.Column{Value: &shim.Column_Int64{Int64: rateKwh}},
		},
	})

	if !ok && err == nil {
		logger.Errorf("Error in enrolling a new account:%s", err)
		return nil, errors.New("Error in enrolling a new account")
	}
	logger.Infof("Enrolled account %s", accountId)

	return nil, nil
}

// Deletes an existing meter
func (t *EnergyTradingChainCode) delete(stub *shim.ChaincodeStub, args []string) ([]byte, error) {
	logger.Info("In delete function")
	if len(args) != 1 {
		logger.Error("Incorrect number of arguments")
		return nil, errors.New("Incorrect number of arguments. Specify account number to be deleted")
	}

	accountId := args[0]

	logger.Infof("Deleting meter with id:%s", accountId)

	var columns []shim.Column
	col1 := shim.Column{Value: &shim.Column_String_{String_: accountId}}
	columns = append(columns, col1)
	err := stub.DeleteRow(tableName, columns)
	if err != nil {
		logger.Errorf("Error in deleting an account:%s", err)
		return nil, errors.New("Error in deleting an account")
	}
	logger.Infof("Deleted account %s", accountId)

	return nil, nil
}

func (t *EnergyTradingChainCode) getRow(stub *shim.ChaincodeStub, accountId string) (shim.Row, error) {
	var columns []shim.Column
	col1 := shim.Column{Value: &shim.Column_String_{String_: accountId}}
	columns = append(columns, col1)

	return stub.GetRow(tableName, columns)
}

func (t *EnergyTradingChainCode) updateRow(stub *shim.ChaincodeStub, row shim.Row) (bool, error) {
	return stub.ReplaceRow(tableName, row)
}

// Change account balance. +ve value means deposit and -ve value means withdrawal
func (t *EnergyTradingChainCode) changeAccountBalance(stub *shim.ChaincodeStub, args []string) ([]byte, error) {
	logger.Info("In changeAccountBalance function")
	if len(args) < 2 {
		logger.Error("Incorrect number of arguments")
		return nil, errors.New("Incorrect number of arguments. Specify account number and fund to be deposited")
	}

	accountId := args[0]
	amountToBeDeposited := args[1]

	logger.Debugf("Adding %s coins to meter with id:%s", amountToBeDeposited, accountId)
	numCoins, err := strconv.ParseFloat(string(amountToBeDeposited), 64)
	if err != nil {
		logger.Errorf("Error in converting to float:%s", err.Error())
		return nil, fmt.Errorf("Invalid value of amount to be deposited:%s", amountToBeDeposited)
	}

	row, err := t.getRow(stub, accountId)
	if err != nil {
		logger.Errorf("Failed retrieving account [%s]: [%s]", accountId, err)
		return nil, fmt.Errorf("Failed retrieving account [%s]: [%s]", accountId, err)
	}
	prevBalanceStr := row.Columns[3].GetString_()
	logger.Debugf("Previous balance for account:%s is %s", accountId, prevBalanceStr)
	prevBalance, err := strconv.ParseFloat(string(prevBalanceStr), 64)
	if err != nil {
		logger.Errorf("Error in converting to float:%s", err.Error())
		return nil, fmt.Errorf("Invalid value of accountBalance:%s", prevBalanceStr)
	}
	newBalance := prevBalance + numCoins
	logger.Debugf("New balance for account:%s is %f", accountId, newBalance)
	newBalanceStr := strconv.FormatFloat(newBalance, 'f', 6, 64)
	row.Columns[3] = &shim.Column{Value: &shim.Column_String_{String_: newBalanceStr}}

	ok, err := t.updateRow(stub, row)
	if !ok && err == nil {
		logger.Errorf("Error in updating account:%s with balance:%s", accountId, newBalanceStr)
		return nil, errors.New("Error in updating account")
	}
	logger.Infof("Changed account balance for account: %s", accountId)

	return nil, nil
}

// Report energy produced or consumed. +ve value means produced and -ve value means consumed
func (t *EnergyTradingChainCode) reportDelta(stub *shim.ChaincodeStub, args []string) ([]byte, error) {
	logger.Info("In reportDelta function")
	if len(args) < 2 {
		logger.Error("Incorrect number of arguments")
		return nil, errors.New("Incorrect number of arguments. Specify account number and fund to be deposited")
	}

	accountId := args[0]
	amountKwhReported := args[1]

	logger.Debugf("Accumulating energy reported %s kwh to meter with id:%s", amountKwhReported, accountId)
	reportedKwhDelta, err := strconv.ParseInt(string(amountKwhReported), 10, 64)
	if err != nil {
		logger.Errorf("Error in converting to int:%s", err.Error())
		return nil, fmt.Errorf("Invalid value of reported kwh to be accumulated:%s", amountKwhReported)
	}

	row, err := t.getRow(stub, accountId)
	if err != nil {
		logger.Errorf("Failed retrieving account [%s]: [%s]", accountId, err)
		return nil, fmt.Errorf("Failed retrieving account [%s]: [%s]", accountId, err)
	}
	prevBalance := row.Columns[2].GetInt64()
	logger.Debugf("Previous reported kwh for account:%s is %d", accountId, prevBalance)
	newBalance := prevBalance + reportedKwhDelta
	logger.Debugf("New reported kwh for account:%s is %d", accountId, newBalance)
	row.Columns[2] = &shim.Column{Value: &shim.Column_Int64{Int64: newBalance}}

	ok, err := t.updateRow(stub, row)
	if !ok && err == nil {
		logger.Errorf("Error in updating reported kwh:%s with balance:%d", accountId, newBalance)
		return nil, errors.New("Error in updating account")
	}
	logger.Infof("Changed reported kwh for account: %s", accountId)

	return nil, nil
}

// Settles the accounts and resets the reported kwh back to 0 for all meters
func (t *EnergyTradingChainCode) settle(stub *shim.ChaincodeStub, args []string) ([]byte, error) {
	logger.Info("In settle function")
	var columns []shim.Column

	rowChannel, err := stub.GetRows(tableName, columns)
	if err != nil {
		logger.Errorf("Error in getting rows:%s", err.Error())
		return nil, errors.New("Error in fetching rows")
	}
	meters := make([]*MeterInfo, 0)
	for row := range rowChannel {
		balance, err := strconv.ParseFloat(row.Columns[3].GetString_(), 64)
		if err != nil {
			logger.Errorf("Error in converting to float:%s", err.Error())
			return nil, fmt.Errorf("Invalid value of accountBalance:%s", row.Columns[3].GetString_())
		}
		meter := MeterInfo{
			Id:             row.Columns[0].GetString_(),
			Name:           row.Columns[1].GetString_(),
			Kwh:            row.Columns[2].GetInt64(),
			AccountBalance: balance,
			RatePerKwh:     row.Columns[4].GetInt64(),
		}
		meters = append(meters, &meter)
	}
	logger.Infof("Number of rows in table:%d", len(meters))

	xchngRateStr, err := stub.GetState("exchange_rate")
	if err != nil {
		logger.Error("Failed to retrieve exchange rate")
		return nil, fmt.Errorf("Failed to retrieve exchange rate")
	}

	xchngRate, err := strconv.ParseFloat(string(xchngRateStr), 64)
	if err != nil {
		logger.Errorf("Invalid value %s for exchange rate", xchngRateStr)
		return nil, errors.New("Invalid value for exchange rate")
	}
	logger.Debugf("Smart contract will charge producers at rate of %f", xchngRate)

	xchngBalanceStr, err := stub.GetState("exchange_account_balance")
	if err != nil...