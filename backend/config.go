package main

type Config struct {
	Port    int
	Host    string
	DataDir string
}

var config Config = Config{
	Port:    8090,
	Host:    "localhost",
	DataDir: "./data",
}
