package process

import (
	"fmt"
	"github.com/DistributedClocks/GoVector/govec"
	"net"
	"sdccProject/src/utils"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	Period = 2000 * time.Millisecond
)

type Process struct {
	Info           utils.Node
	NetLayout      utils.NetLayout
	Balance        int
	FullState      utils.FullState
	Listener       net.Listener
	CurrentStateCh chan utils.FullState
	RecvStateCh    chan utils.FullState
	MarkCh         utils.MarkChannel
	SendMsgCh      chan utils.AppMessage
	AppMsgCh       utils.AppMsgChannel
	Logger         *utils.Logger
	Mutex          sync.Mutex
}

func NewProcess(netIdx int, currentStateCh chan utils.FullState, recvStateCh chan utils.FullState, markCh utils.MarkChannel, sendMsgCh chan utils.AppMessage, netLayout utils.NetLayout, logger *utils.Logger) *Process {
	var myNode = netLayout.Nodes[netIdx]

	appMsgCh := utils.AppMsgChannel{
		SendCh: make(chan utils.RespMessage, 10),
		RecvCh: make(chan utils.AppMessage, 10),
	}

	// Open Listener port
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(myNode.Port))
	if err != nil {
		panic(fmt.Sprintf("ERROR: unable to open port: %s. Error: %s.", strconv.Itoa(myNode.Port), err))
	}

	var myProcess = Process{
		Info:           myNode,
		NetLayout:      netLayout,
		Balance:        netLayout.InitialBalance,
		FullState:      utils.FullState{},
		Listener:       listener,
		CurrentStateCh: currentStateCh,
		RecvStateCh:    recvStateCh,
		MarkCh:         markCh,
		SendMsgCh:      sendMsgCh,
		AppMsgCh:       appMsgCh,
		Logger:         logger,
		Mutex:          sync.Mutex{},
	}
	myProcess.Logger.Trace.Printf("Listening on port: %s. Initial balance : $%d", strconv.Itoa(myNode.Port), myProcess.Balance)

	go myProcess.sender()
	go myProcess.receiver()
	return &myProcess
}

func (p *Process) sender() {
	opts := govec.GetDefaultLogOptions()
	var outBuf []byte
	outBuf = []byte{'A', 'B'}
	for {
		select {
		case respMsg := <-p.AppMsgCh.SendCh:
			p.sendAppMsg(respMsg, outBuf, opts)
		case <-p.MarkCh.SendCh:
			// Send markers
			p.sendMarkers(opts)
		case state := <-p.CurrentStateCh:
			p.updateState(state, opts)
		}
	}
}

func (p *Process) receiver() *utils.Message {
	var conn net.Conn
	var err error
	var recvData []byte
	recvData = make([]byte, 1024)

	for {
		p.Logger.Trace.Println("Waiting for connection accept...")
		if conn, err = p.Listener.Accept(); err != nil {
			p.Logger.Error.Panicf("Server accept connection error: %s", err)
		}
		nBytes, err := conn.Read(recvData[0:])
		if err != nil {
			p.Logger.Error.Panicf("Reading messagge error: %s", err)
		}
		if !strings.Contains(string(recvData[0:nBytes]), "Channels") {
			// Waiting for MSG or marks
			var recvMsg utils.AppMessage
			p.Logger.GoVector.UnpackReceive("Receiving Message", recvData[0:nBytes], &recvMsg, govec.GetDefaultLogOptions())
			// Send data to snapshot
			if recvMsg.IsMarker {
				p.Logger.Info.Printf("MARK recv from: %s\n", p.NetLayout.Nodes[recvMsg.From].Name)
			} else {
				p.Logger.GoVector.LogLocalEvent(fmt.Sprintf("MSG %s, content: $%d, from [%s]", recvMsg.Msg.ID, recvMsg.Msg.Body, p.NetLayout.Nodes[recvMsg.From].Name), govec.GetDefaultLogOptions())
				p.UpdateBalance(recvMsg.Msg.Body, "received")
				p.Logger.Info.Printf("MSG %s [Amount: %d] received from: %s\n", recvMsg.Msg.ID, recvMsg.Msg.Body, p.NetLayout.Nodes[recvMsg.From].Name)
				p.AppMsgCh.RecvCh <- recvMsg
			}
			p.MarkCh.RecvCh <- recvMsg
		} else {
			var tempState = utils.FullState{}
			p.Logger.GoVector.UnpackReceive("Receiving State", recvData[0:nBytes], &tempState, govec.GetDefaultLogOptions())
			p.Logger.Info.Println("State recv from: ", tempState.Node.NodeName)
			// Send state to snapshot
			p.RecvStateCh <- tempState
		}
	}
}

func (p *Process) sendAppMsg(msg utils.RespMessage, outBuf []byte, opts govec.GoLogOptions) {
	detMsg := msg.AppMsg
	responseCh := msg.RespCh
	p.Mutex.Lock()
	locState := p.FullState
	p.Mutex.Unlock()
	if !locState.Node.Busy { // it is not performing a global snapshot
		if detMsg.Msg.Body > p.getBalance() {
			p.Logger.Error.Panicln("Cannot send app msg: not enough money!")
		}
		detMsg.From = p.Info.Idx
		outBuf = p.Logger.GoVector.PrepareSend(fmt.Sprintf("Sending msg %s, content: $%d", detMsg.Msg.ID, detMsg.Msg.Body), detMsg, opts)
		node := p.NetLayout.Nodes[detMsg.To]
		if node.Name != p.Info.Name {
			go p.sendDirectMsg(outBuf, node)
		}
		p.UpdateBalance(detMsg.Msg.Body, "sent")
		p.SendMsgCh <- detMsg
		responseCh <- utils.NewAppMsg("", -1, -1, -1)
		p.Logger.Info.Printf("MSG %s [Amount: %d] sent to: %s. Current budget: $%d\n", detMsg.Msg.ID, detMsg.Msg.Body, p.NetLayout.Nodes[detMsg.To].Name, p.getBalance())
	} else {
		p.Logger.Warning.Println("Cannot send app msg while node is performing global snapshot")
		responseCh <- detMsg
	}
}

func (p *Process) sendMarkers(opts govec.GoLogOptions) {
	mark := utils.NewMarkMsg(p.Info.Idx)
	outBuf := p.Logger.GoVector.PrepareSend("Sending mark", mark, opts)
	for _, node := range p.NetLayout.Nodes {
		if node.Name != p.Info.Name {
			go p.sendDirectMsg(outBuf, node)
		}
	}
}

func (p *Process) updateState(state utils.FullState, opts govec.GoLogOptions) {
	state.Node.Balance = p.getBalance()
	p.Mutex.Lock()
	p.FullState = state
	p.Mutex.Unlock()
	p.Logger.Info.Println("Node state update", p.FullState.String()) // DEBUG
	if state.AllMarksRecv {
		outBuf := p.Logger.GoVector.PrepareSend("Sending my state to all...", state, opts)
		for _, node := range p.NetLayout.Nodes {
			if node.Name != p.Info.Name {
				p.Logger.Info.Printf("Sending state to: %s\n", node.Name)
				go p.sendDirectMsg(outBuf, node)
			}
		}
	}
}

func (p *Process) sendDirectMsg(msg []byte, node utils.Node) {
	var conn net.Conn
	var err error

	netAddr := fmt.Sprint(node.IP + ":" + strconv.Itoa(node.Port))
	conn, err = net.Dial("tcp", netAddr)
	for i := 0; err != nil && i < p.NetLayout.SendAttempts; i++ {
		p.Logger.Warning.Printf("Client connection error: %s", err)
		time.Sleep(Period)
		conn, err = net.Dial("tcp", netAddr)
	}
	if err != nil || conn == nil {
		p.Logger.Error.Panicf("Client connection error: %v", err)
	}
	_, err = conn.Write(msg)
	if err != nil {
		p.Logger.Error.Panicf("Sending data error: %v", err)
	}
	err = conn.Close()
	if err != nil {
		p.Logger.Error.Panicf("Closing connection error: %v", err)
	}
}

func (p *Process) UpdateBalance(amount int, status string) {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	switch status {
	case "sent":
		p.Balance -= amount
	case "received":
		p.Balance += amount
	}
}

func (p *Process) getBalance() int {
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.Balance
}
