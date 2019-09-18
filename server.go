package main

import (
	"fmt"
	"net"
	"net/http"
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

func connectTester(c echo.Context) error {
	host := c.Param("host")
	port := c.Param("port")

	results := rawConnect(host, []string{port})

	return c.JSON(http.StatusOK, results)
}

func rawConnect(host string, ports []string) []connectResult {
	results := make([]connectResult, len(ports), len(ports))
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
