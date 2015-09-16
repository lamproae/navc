/*
 * Copyright 2015 Google Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/* TODO: We have a problem with file dependencies. What if header files change?
 * All files dependent to this one should also be updated recursively. What
 * if the header is removed? Where would all the symbols go? What if the header
 * shows up again?
 *
 * We need to solve all this issues and it may require plenty of changes in the
 * symbols DB.
 */

package main

import (
	"flag"
	"log"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"os/signal"
	"sync"
)

func main() {
	// path to symbols DB
	var dbFile string
	flag.StringVar(&dbFile, "db", ".navc_dbsymbols", "Path to symbols path")

	// number of parallel indexing threads
	var nIndexingThreads int
	flag.IntVar(&nIndexingThreads, "numThreads", 1,
		"Number of indexing threads (don't use)")

	// reset DB
	var resetDb bool
	flag.BoolVar(&resetDb, "resetDb", false,
		"Reset symbols DB and start over")

	flag.Parse()

	// socket file for communication with daemon
	socketFile := "/tmp/navc.sock"

	// list of directores with source to index
	var indexDir []string
	if len(flag.Args()) > 0 {
		indexDir = flag.Args()
		for _, path := range indexDir {
			fi, err := os.Stat(path)
			if err != nil {
				log.Println("unable to access ", path, err)
				return
			}
			if !fi.IsDir() {
				log.Println("only dir inputs allowed")
				return
			}
		}
	} else {
		indexDir = []string{"."}
	}

	// handle interrup and kill signals
	intr := make(chan os.Signal, 1)
	signal.Notify(intr, os.Interrupt, os.Kill)
	defer close(intr)

	var wg sync.WaitGroup
	defer wg.Wait()

	// if we need to reset the database, erase the old one
	if resetDb {
		os.Remove(dbFile)
	}

	// open databased of symbols
	db := NewDBConnFactory(dbFile)
	defer db.Close()

	// create parser
	parser := NewParser(db, indexDir)

	// start files handler
	StartFilesHandler(indexDir, nIndexingThreads, parser, db)
	defer CloseFilesHandler()

	// start serving requests
	os.Remove(socketFile)
	lis, err := net.Listen("unix", socketFile)
	if err != nil {
		log.Println("error opening socket", err)
		return
	}
	defer os.Remove(socketFile)
	defer lis.Close()

	handler := rpc.NewServer()
	rd := db.NewReader()
	handler.Register(&RequestHandler{rd})
	go func() {
		wg.Add(1)
		defer wg.Done()

		for {
			conn, err := lis.Accept()
			if err != nil {
				log.Println("accepting connection (breaking):",
					err)
				return
			}

			codec := jsonrpc.NewServerCodec(conn)
			err = handler.ServeRequest(codec)
			if err != nil {
				log.Println("handling request (ignoring):", err)
			}
			codec.Close()
		}
	}()
	defer rd.Close()

	// wait until ctl-c is pressed
	select {
	case <-intr:
	}
}
