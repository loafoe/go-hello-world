package main

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	listenString := ":8080"

	// Echo instance
	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	e.GET("/", hello)
	e.GET("/api/test/:host/:port", connectTester)
	e.GET("/api/dump/:base64_path", fileDumper)
	e.Any("/dump", requestDumper)

	if port := os.Getenv("PORT"); port != "" {
		listenString = ":" + port
	}

	e.Logger.Fatal(e.Start(listenString))
}

type connectResult struct {
	IP     string
	Port   string
	Status string
}

// Handler
func hello(c echo.Context) error {
	return c.String(http.StatusOK, fmt.Sprintf("Hello! You've requested: %s", c.Request().RequestURI))
}

func requestDumper(c echo.Context) error {
	dump, err := httputil.DumpRequest(c.Request(), true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
		return err
	}
	return c.String(http.StatusOK, string(dump))
}

func fileDumper(c echo.Context) error {
	data, err := base64.StdEncoding.DecodeString(c.Param("base64_path"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
		return err
	}
	filename := string(data)
	data, err = ioutil.ReadFile(filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, err)
		return err
	}
	_, err = c.Response().Write(data)
	return err
}

func connectTester(c echo.Context) error {
	host := c.Param("host")
	port := c.Param("port")

	results := rawConnect(host, []string{port})

	return c.JSON(http.StatusOK, results)
}

func rawConnect(host string, ports []string) []connectResult {
	results := make([]connectResult, len(ports))
	for i, port := range ports {
		timeout := time.Second
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
		if err != nil {
			results[i] = connectResult{
				IP:     host,
				Port:   port,
				Status: fmt.Sprintf("Connection error: %s", err),
			}
		}
		if conn != nil {
			defer conn.Close()
			results[i] = connectResult{
				IP:     host,
				Port:   port,
				Status: fmt.Sprintf("Open"),
			}
		}
	}
	return results
}
