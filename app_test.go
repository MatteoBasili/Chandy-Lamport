package main_test

import (
	"encoding/gob"
	"fmt"
	"github.com/DistributedClocks/GoVector/govec"
	"github.com/DistributedClocks/GoVector/govec/vrpc"
	"math/rand"
	"net/rpc"
	"os"
	"sdccProject/src/utils"
	"strconv"
	"testing"
	"time"
)

const (
	appName    = "node_app"
	lowerBound = 0
	upperBound = 100
)

var RPCConn map[string]*rpc.Client

// Connect and initialize RPC nodes
func TestMain(m *testing.M) {
	fmt.Println("Starting tests... ")
	setupNetwork()
	fmt.Println("Execute the tests...")
	code := m.Run()
	terminate()
	os.Exit(code)
}

func setupNetwork() {

	var netLayout utils.NetLayout
	netLayout = utils.ReadConfig()
	if len(netLayout.Nodes) < 2 {
		panic("At least 2 processes are needed")
	}
	fmt.Printf("Net layout: %v\n", netLayout.Nodes)

	gob.Register(utils.AppMessage{})
	// Start govec logger
	config := govec.GetDefaultConfig()
	config.UseTimestamps = true
	logger := govec.InitGoVector("T", utils.OutputDir+"GoVector/LogFileTest", config)

	RPCConn = make(map[string]*rpc.Client)

	for idx, node := range netLayout.Nodes {
		// Initialize RPC node
		go utils.RunCommand("go", "run", appName+".go", strconv.Itoa(idx), strconv.Itoa(node.AppPort))

		// Connect via RPC to the server
		var clientRPC *rpc.Client
		var err error
		for i := 0; i < netLayout.SendAttempts; i++ {
			time.Sleep(3 * time.Second) // Wait for RPC initialization
			clientRPC, err = vrpc.RPCDial("tcp", node.IP+":"+strconv.Itoa(node.AppPort), logger, govec.GetDefaultLogOptions())
			if err == nil {
				break
			}
		}
		if err != nil {
			panic(err)
		}
		RPCConn[node.Name] = clientRPC

	}
}

func terminate() {
	fmt.Println("Tests finished. Closing connections...")
	for _, conn := range RPCConn {
		_ = conn.Close()
	}
	fmt.Println("Connections terminated")
	fmt.Println("Terminating all processes...")
	utils.RunCommand("taskkill", "/F", "/IM", appName+".exe")
}

func genCasNum(min int, max int) int {
	rand.New(rand.NewSource(time.Now().UnixNano()))
	randomInt := rand.Intn(max-min+1) + min
	return randomInt
}

func TestMsg(t *testing.T) {
	nMsgs := 6
	respMsgCh := make(chan int, nMsgs)

	go func() {
		for i := 0; i < nMsgs; i++ {
			msgN := <-respMsgCh
			fmt.Printf("Msg nº: %d sent\n", msgN)
		}
		fmt.Println("All messages sent.")
	}()

	msg1 := utils.NewAppMsg("MS1", genCasNum(lowerBound, upperBound), 0, 1)
	go utils.RunRPCCommand("NodeApp.SendAppMsg", RPCConn["P0"], msg1, 1, respMsgCh)
	fmt.Println("Test: ordered 1st msg")

	msg2 := utils.NewAppMsg("MS2", genCasNum(lowerBound, upperBound), 0, 1)
	go utils.RunRPCCommand("NodeApp.SendAppMsg", RPCConn["P0"], msg2, 2, respMsgCh)
	fmt.Println("Test: ordered 2nd msg")

	msg3 := utils.NewAppMsg("MS3", genCasNum(lowerBound, upperBound), 0, 2)
	go utils.RunRPCCommand("NodeApp.SendAppMsg", RPCConn["P0"], msg3, 3, respMsgCh)
	fmt.Println("Test: ordered 3rd msg")

	msg4 := utils.NewAppMsg("MS4", genCasNum(lowerBound, upperBound), 2, 0)
	go utils.RunRPCCommand("NodeApp.SendAppMsg", RPCConn["P2"], msg4, 4, respMsgCh)
	fmt.Println("Test: ordered 4th msg")

	msg5 := utils.NewAppMsg("MS5", genCasNum(lowerBound, upperBound), 1, 0)
	go utils.RunRPCCommand("NodeApp.SendAppMsg", RPCConn["P1"], msg5, 5, respMsgCh)
	fmt.Println("Test: ordered 5th msg")

	msg6 := utils.NewAppMsg("MS6", genCasNum(lowerBound, upperBound), 2, 1)
	go utils.RunRPCCommand("NodeApp.SendAppMsg", RPCConn["P2"], msg6, 6, respMsgCh)
	fmt.Println("Test: ordered 6th msg")

	time.Sleep(5 * time.Second)
	fmt.Println("Done!")
}