/*
Copyright 2014 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/negroni"
)

var masterClient *redis.Client

func ListRangeHandler(rw http.ResponseWriter, req *http.Request) {
	key := mux.Vars(req)["key"]
	cmd := masterClient.LRange(req.Context(), key, 0, -1)
	members := HandleError(cmd.Val(), cmd.Err()).([]string)
	membersJSON := HandleError(json.MarshalIndent(members, "", "  ")).([]byte)
	rw.Write(membersJSON)
}

func ListPushHandler(rw http.ResponseWriter, req *http.Request) {
	key := mux.Vars(req)["key"]
	value := mux.Vars(req)["value"]
	HandleError(nil, masterClient.RPush(req.Context(), key, value).Err())
	ListRangeHandler(rw, req)
}

func InfoHandler(rw http.ResponseWriter, req *http.Request) {
	cmd := masterClient.Info(req.Context())
	info := HandleError(cmd.Val(), cmd.Err()).(string)
	rw.Write([]byte(info))
}

func EnvHandler(rw http.ResponseWriter, req *http.Request) {
	environment := make(map[string]string)
	for _, item := range os.Environ() {
		splits := strings.Split(item, "=")
		key := splits[0]
		val := strings.Join(splits[1:], "=")
		environment[key] = val
	}

	envJSON := HandleError(json.MarshalIndent(environment, "", "  ")).([]byte)
	rw.Write(envJSON)
}

func HealthHandler(rw http.ResponseWriter, req *http.Request) {
	if err := masterClient.Ping(req.Context()).Err(); err != nil {
		redisUp.Set(0)
		rw.WriteHeader(http.StatusInternalServerError)
		rw.Write([]byte(err.Error()))
	} else {
		redisUp.Set(1)
		rw.WriteHeader(http.StatusOK)
	}
}

func HandleError(result interface{}, err error) (r interface{}) {
	if err != nil {
		panic(err)
	}
	return result
}

const listenAddr = ":3000"

func main() {
	redis.SetLogger(redisLogger{})
	masterClient = redis.NewClient(&redis.Options{
		Addr: os.Getenv("REDIS_HOST") + ":6379",
	})
	defer masterClient.Close()

	ctx := context.Background()
	shutdownTracing, err := initTracing(ctx, masterClient)
	if err != nil {
		logger.Error("tracing setup failed", "error", err)
		os.Exit(1)
	}
	defer shutdownTracing(ctx)

	r := mux.NewRouter()
	r.Path("/lrange/{key}").Methods("GET").HandlerFunc(ListRangeHandler)
	r.Path("/rpush/{key}/{value}").Methods("GET").HandlerFunc(ListPushHandler)
	r.Path("/info").Methods("GET").HandlerFunc(InfoHandler)
	r.Path("/env").Methods("GET").HandlerFunc(EnvHandler)
	r.Path("/healthz").Methods("GET").HandlerFunc(HealthHandler)
	r.Path("/metrics").Methods("GET").Handler(promhttp.Handler())

	// negroni's text Logger is replaced by the JSON log line written in MetricsMiddleware
	n := negroni.New(negroni.NewRecovery(), MetricsMiddleware(r), negroni.NewStatic(http.Dir("public")))
	n.UseHandler(r)

	// Tracing wraps everything, so the span exists when MetricsMiddleware logs the trace_id
	logger.Info("listening", "addr", listenAddr)
	if err := http.ListenAndServe(listenAddr, withTracing(n)); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
