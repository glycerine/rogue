package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	cv "github.com/glycerine/goconvey/convey"
)

func Test002ClientServerOverWebsocketShouldCommunicate(t *testing.T) {
	log.SetFlags(0)

	cv.Convey("our client and our server should be able to communicate over websockets", t, func() {
		cfg.testSetup()

		// start server
		mux := http.NewServeMux()
		mux.HandleFunc("/echo", echo)
		//mux.HandleFunc("/", home)
		server := NewWebServer(cfg.Addr, mux)
		server.Start()

		// start client
		cli := NewWsClient(&cfg)
		err := cli.Start()
		panicOn(err)

		time.Sleep(100 * time.Millisecond)
		fmt.Printf("\n test: done with sleep.\n")
		cli.Stop()
		fmt.Printf("\n test: done with cli.Stop().\n")
		server.Stop()
		fmt.Printf("\n test: done with server.Stop().\n")
	})
}

type TestConfig struct {
	Port int
	Host string
	Addr string
}

// allow client to find server
var cfg TestConfig

func (testCfg *TestConfig) testSetup() {
	port := GetAvailPort()
	host := "127.0.0.1"
	testCfg.Port = port
	testCfg.Host = host
	testCfg.Addr = fmt.Sprintf("%v:%v", host, port)
}

type Goro struct {
	ReqStop chan bool
	Done    chan bool

	// mut protects the variables below it
	mut    sync.Mutex
	isDone bool
	err    error // any unusual err storred here. Nil on normal shutdown.
}

func (g *Goro) Stop() {
	if g.IsDone() {
		return
	}

	// don't be holding mut during
	// this send and receive wait.
	g.ReqStop <- true
	<-g.Done

	g.mut.Lock()
	g.isDone = true
	g.mut.Unlock()
}

func (g *Goro) IsDone() bool {
	g.mut.Lock()
	defer g.mut.Unlock()
	return g.isDone
}

func (g *Goro) GetErr() error {
	g.mut.Lock()
	defer g.mut.Unlock()
	return g.err
}

func (g *Goro) SetErr(err error) {
	g.mut.Lock()
	defer g.mut.Unlock()
	g.err = err
}

type ServerSender struct {
	Goro
	Conn *websocket.Conn
}

func NewServerSender(conn *websocket.Conn) *ServerSender {
	p := &ServerSender{
		Goro: Goro{
			ReqStop: make(chan bool),
			Done:    make(chan bool),
		},
		Conn: conn,
	}
	return p
}

func (p *ServerSender) Shutdown() {

}

// do a bunch of sends more often than we have requests.
func (p *ServerSender) Start() {
	rnd := rand.New(rand.NewSource(99))
	go func() {
		defer func() {
			p.Shutdown()
			close(p.Done)
		}()

		for {
			select {
			case <-p.ReqStop:
				return
			case <-time.After(2 * time.Millisecond):
				msg := fmt.Sprintf("This is a random extra message from ServerSender, number '%v'", rnd.Int63())
				fmt.Printf("\n ServerSender sending '%s'\n", msg)
				p.Conn.SetWriteDeadline(time.Now().Add(writeWait))
				err := p.Conn.WriteMessage(websocket.TextMessage, []byte(msg))
				if err != nil {
					fmt.Printf("echo() WriteMessage error: '%v'", err)
				}

			}
		}
	}()
}

func echo(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)

	c.SetReadLimit(maxMessageSize)
	c.SetPongHandler(func(string) error { c.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	if err != nil {
		fmt.Printf("echo() upgrader.Upgrade() error: '%v'", err)
		return
	}
	defer c.Close()

	xtraSend := NewServerSender(c)
	xtraSend.Start()
	fmt.Printf("\n called xtraSend.Start()\n")
	defer xtraSend.Stop()

	for {
		c.SetReadDeadline(time.Now().Add(pongWait))
		mt, message, err := c.ReadMessage()
		if err != nil {
			fmt.Printf("echo() ReadMessage error: '%v'", err)
			break
		}

		fmt.Printf("echo server recv: %s", message)

		c.SetWriteDeadline(time.Now().Add(writeWait))
		err = c.WriteMessage(mt, message)
		if err != nil {
			fmt.Printf("echo() WriteMessage error: '%v'", err)
			break
		}
	}
}

//func home(w http.ResponseWriter, r *http.Request) {
//	homeTemplate.Execute(w, "ws://"+r.Host+"/echo")
//}

//  client

type WsClient struct {
	ReqStop    chan bool
	ReaderDone chan bool
	WriterDone chan bool
	Done       chan bool
	Addr       string
}

func NewWsClient(cfg *TestConfig) *WsClient {
	p := &WsClient{
		ReqStop:    make(chan bool),
		ReaderDone: make(chan bool),
		WriterDone: make(chan bool),
		Done:       make(chan bool),
		Addr:       cfg.Addr,
	}
	return p
}

// only call Stop() once
func (p *WsClient) Stop() {
	close(p.ReqStop)
	<-p.Done
}
func (p *WsClient) Start() error {

	log.SetFlags(0)

	u := url.URL{Scheme: "ws", Host: p.Addr, Path: "/echo"}
	fmt.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		fmt.Printf("dial:", err)
		return err
	}

	c.SetReadLimit(maxMessageSize)
	c.SetPongHandler(func(string) error { c.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	// closer calls c.Close() when both reader and writer are done.
	// This insures proper sequencing of shutdown. We close p.Done
	// to signal both reader and writer are done.
	go func() {
		wDone := false
		rDone := false

		// make nilable references to the channels
		chWd := p.WriterDone
		chRd := p.ReaderDone

		for {
			select {
			case <-chWd:
				fmt.Printf("\n client close monitor sees WriterDone\n")
				wDone = true
				chWd = nil
			case <-chRd:
				fmt.Printf("\n client close monitor sees ReaderDone\n")
				rDone = true
				chRd = nil
			}
			if rDone && wDone {
				c.Close()
				fmt.Printf("\n client close monitor sees both done! closing p.Done\n")
				close(p.Done)
				return
			}
		}
	}()

	// reader: consume replies mechanically
	go func() {
		defer close(p.ReaderDone)

		for {
			fmt.Printf("\n client reader: at top of read loop\n")
			select {
			case <-p.ReqStop:
				fmt.Printf("\n client reader got ReqStop, exiting.\n")
				return
			default:
			}

			c.SetReadDeadline(time.Now().Add(pongWait))
			_, message, err := c.ReadMessage()
			if err != nil {
				fmt.Printf("\n client reader error: '%v'", err)
				return
			}
			fmt.Printf("\n client reader recv: %s", message)
		}
	}()

	go func() {
		// test writer: every 1 millisecond tick, send a message
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		defer close(p.WriterDone)

		for {
			select {
			case t := <-ticker.C:
				fmt.Printf("\n client writer: ticker.C fired: writing time.\n")
				c.SetWriteDeadline(time.Now().Add(writeWait))
				err := c.WriteMessage(websocket.TextMessage, []byte(t.String()))
				if err != nil {
					fmt.Printf("tick every second test writer error: '%v'", err)
					return
				}
			case <-p.ReqStop:
				fmt.Printf("\n client writer got ReqStop, exiting.\n")

				fmt.Println("stop request")
				// To cleanly close a connection, a client should send a close
				// frame and wait for the server to close the connection.
				err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				if err != nil {
					fmt.Printf("client write close got error: '%v'", err)
					return
				}
				return
			}
		}
		return
	}()
	return nil
}
