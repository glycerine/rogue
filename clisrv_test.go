package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
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

		time.Sleep(5 * time.Second)
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

func echo(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("echo() upgrader.Upgrade() error: '%v'", err)
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Printf("echo() ReadMessage error: '%v'", err)
			break
		}
		log.Printf("recv: %s", message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Println("write:", err)
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
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Printf("dial:", err)
		return err
	}

	// closer calls c.Close() when both reader and writer are done.
	// This insures proper sequencing of shutdown. We close p.Done
	// to signal both reader and writer are done.
	go func() {
		wDone := false
		rDone := true

		// make nilable references to the channels
		chWd := p.WriterDone
		chRd := p.ReaderDone
		
		for {
			select {
			case <-chWd:
				fmt.Printf("\n close monitor sees WriterDone\n")
				wDone = true
				chWd = nil
			case <-chRd:
				fmt.Printf("\n close monitor sees ReaderDone\n")
				rDone = true
				chRd = nil
			}
			if rDone && wDone {
				c.Close()
				close(p.Done)
				return
			}
		}
	}()

	// reader: consume replies mechanically
	go func() {
		defer close(p.ReaderDone)

		for {
			select {
			case <-p.ReqStop:
				fmt.Printf("\n reader got ReqStop, exiting.\n")
				return
			}

			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read error:", err)
				return
			}
			log.Printf("recv: %s", message)
		}
	}()

	go func() {
		// test writer: every 1 second tick, send a message
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		defer close(p.WriterDone)
		
		for {
			select {
			case t := <-ticker.C:
				fmt.Printf("\n writer: ticker.C fired: writing time.\n")			
				err := c.WriteMessage(websocket.TextMessage, []byte(t.String()))
				if err != nil {
					log.Println("write:", err)
					return
				}
			case <-p.ReqStop:
				fmt.Printf("\n writer got ReqStop, exiting.\n")
				
				log.Println("stop request")
				// To cleanly close a connection, a client should send a close
				// frame and wait for the server to close the connection.
				err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				if err != nil {
					log.Println("write close:", err)
					return
				}
				return
			}
		}
		return
	}()
	return nil
}
