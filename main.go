package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

var (
	katago   *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	gameLock sync.Mutex
	gameOn   bool
)

func startGame(c *gin.Context) {
	gameLock.Lock()
	defer gameLock.Unlock()

	if gameOn {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Game already running"})
		return
	}

	var params struct {
		BoardSize int     `json:"boardsize"`
		Komi      float64 `json:"komi"`
		Config    string  `json:"config"`
		Model     string  `json:"model"`
	}

	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	katago = exec.Command("./katago", "gtp", "-model", params.Model, "-config", params.Config)
	var err error
	stdin, err = katago.StdinPipe()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	stdoutPipe, err := katago.StdoutPipe()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stdout = bufio.NewReader(stdoutPipe)

	if err := katago.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sendGTP(fmt.Sprintf("boardsize %d", params.BoardSize))
	sendGTP(fmt.Sprintf("komi %.1f", params.Komi))
	sendGTP("clear_board")

	gameOn = true
	c.JSON(http.StatusOK, gin.H{"message": "Game started"})
}

func sendGTP(cmd string) string {
	stdin.Write([]byte(cmd + "\n"))
	var resp strings.Builder
	for {
		line, _ := stdout.ReadString('\n')
		if strings.HasPrefix(line, "=") || strings.HasPrefix(line, "?") {
			resp.WriteString(line)
			break
		}
		resp.WriteString(line)
	}
	return strings.TrimSpace(resp.String())
}

func playMove(c *gin.Context) {
	gameLock.Lock()
	defer gameLock.Unlock()

	if !gameOn {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No game running"})
		return
	}

	var params struct {
		Color string `json:"color"`
		Move  string `json:"move"`
	}

	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp := sendGTP(fmt.Sprintf("play %s %s", params.Color, params.Move))

	if strings.HasPrefix(resp, "?") {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Move played", "response": resp})
}

func genMove(c *gin.Context) {
	gameLock.Lock()
	defer gameLock.Unlock()

	if !gameOn {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No game running"})
		return
	}

	var params struct {
		Color string `json:"color"`
	}

	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp := sendGTP(fmt.Sprintf("genmove %s", params.Color))

	if strings.HasPrefix(resp, "?") {
		c.JSON(http.StatusBadRequest, gin.H{"error": resp})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Move generated", "move": strings.Trim(resp, "= ")})
}

func endGame(c *gin.Context) {
	gameLock.Lock()
	defer gameLock.Unlock()

	if gameOn && katago != nil {
		sendGTP("quit")
		katago.Process.Kill()
		katago = nil
		gameOn = false
	}

	c.JSON(http.StatusOK, gin.H{"message": "Game ended"})
}

func main() {
	r := gin.Default()

	r.POST("/start_game", startGame)
	r.POST("/play", playMove)
	r.POST("/genmove", genMove)
	r.POST("/end_game", endGame)

	r.Run(":8080")
}
