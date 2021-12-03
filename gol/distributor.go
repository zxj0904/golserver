package gol

import (
	"fmt"
	"strconv"
	"sync"
	"time"
	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
	keyPresses <-chan rune
}

type CalculateStruct struct {}

type RpcRequest struct {
	World *World
	Turn  int
	StartIndex int
	EndIndex int
}

type RpcResponse struct {
	RpcStatus int
	World     *World
	Turn      int
}

func (this *CalculateStruct) Calculate(req RpcRequest, resp *RpcResponse) error {
	fmt.Printf("start to Calculate, req=%v\n", req)
	//req.World.DebugLog(req.Turn)
	for i := 0; i < req.Turn; i++ {
		req.World.NextStep(req.StartIndex, req.EndIndex)
	}
	//req.World.DebugLog(req.Turn)

	resp.World = req.World
	resp.RpcStatus = 0
	resp.Turn = req.Turn
	return nil
}


type World struct {
	Grid1   *Grid
	Grid2   *Grid
	Width   int
	Height  int
	Threads int
}


type Grid struct {
	Status  [][]bool
	Width   int
	Height  int
	Threads int
}


func NewGrid(w, h, t int) *Grid {
	g := Grid{}
	g.Width = w
	g.Height = h
	g.Threads = t
	g.Status = make([][]bool, h)
	for i := 0; i < h; i++ {
		g.Status[i] = make([]bool, w)
	}

	return &g
}


func NewWorld(w, h, t int) *World {
	world := World{}
	world.Height = h
	world.Width = w
	world.Threads = t
	world.Grid1 = NewGrid(w, h, t)
	world.Grid2 = NewGrid(w, h, t)
	return &world
}


func (g *Grid) Set(x, y int, status bool) {
	g.Status[x][y] = status
}


func (g *Grid) Alive(x, y int) bool {
	x = (x + g.Width) % g.Width
	y = (y + g.Height) % g.Height
	//fmt.Printf("%v %v %v %v\n", x, y, len(g.status), len(g.status[0]))
	return g.Status[x][y]
}


func (g *Grid) NextStatus(x, y int) bool {
	alive := 0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			if (i != 0 || j != 0) && g.Alive(x+i, y+j) {
				alive++
			}
		}
	}

	if alive == 0 {
		//fmt.Printf("x=%v y=%v alive=0\n", x, y)
		return false
	}
	if alive == 3 {
		//fmt.Printf("x=%v y=%v alive=3\n", x, y)
		return true
	}
	return alive == 2 && g.Alive(x, y)
}


func (w *World) NextStep(startIndex, endIndex int) {
	wg := sync.WaitGroup{}
	length := (endIndex - startIndex + 1) / w.Threads
	for i := 0; i < w.Threads; i++ {
		wg.Add(1)
		start := i * length + startIndex
		end := start + length - 1
		if i == w.Threads-1 {
			end = endIndex
		}
		go func(wg *sync.WaitGroup, index, startIndex, endIndex int) {
			defer wg.Done()
			for x := 0; x < w.Height; x++ {
				for y := startIndex; y <= endIndex; y++ {
					w.Grid2.Set(x, y, w.Grid1.NextStatus(x, y))
				}
			}
		}(&wg, i, start, end)
	}
	wg.Wait()

	w.Grid1, w.Grid2 = w.Grid2, w.Grid1
}


func (w *World) imageName1() string {
	return fmt.Sprintf("%sx%s", strconv.Itoa(w.Height), strconv.Itoa(w.Width))
}


func (w *World) imageName2(turn int) string {
	return fmt.Sprintf("%sx%sx%s", strconv.Itoa(w.Height), strconv.Itoa(w.Width), strconv.Itoa(turn))
}


func (w *World) initCell(c distributorChannels) {
	c.ioCommand <- ioInput
	c.ioFilename <- w.imageName1()
	for i := 0; i < w.Height; i++ {
		for j := 0; j < w.Width; j++ {
			tmp := <-c.ioInput
			w.Grid1.Set(i, j, tmp == 255)
		}
	}
}


func (w *World) AliveCount() int {
	count := 0
	for i := 0; i < w.Height; i++ {
		for j := 0; j < w.Width; j++ {
			if w.Grid1.Alive(i, j) {
				count++
			}
		}
	}

	return count
}


func (w *World) DebugLog(turn int) {
	if w.Height > 64 || w.Width > 64 {
		return
	}
	fmt.Printf("-----------turn = %v-----------\n", turn)
	for i := 0; i < w.Height; i++ {
		for j := 0; j < w.Width; j++ {
			if w.Grid1.Alive(i, j) {
				fmt.Printf("@")
			} else {
				fmt.Printf("-")
			}
			fmt.Printf(" ")
		}
		fmt.Printf("\n")
	}
	fmt.Printf("-----------turn = %v-----------\n", turn)
	fmt.Printf("\n")
}


func (w *World) SendFinal(turn int, c distributorChannels) {
	final := FinalTurnComplete{}
	final.CompletedTurns = turn
	final.Alive = make([]util.Cell, 0)

	for y := 0; y < w.Height; y++ {
		for x := 0; x < w.Width; x++ {
			if w.Grid1.Status[y][x] {
				final.Alive = append(final.Alive, util.Cell{X: x, Y: y})
			}
		}
	}

	c.events <- final
}


func (w *World) SendAliveCount(turn int, c distributorChannels) {
	event := AliveCellsCount{}
	event.CompletedTurns = turn
	event.CellsCount = w.AliveCount()

	c.events <- event
}


func (w *World) SendCompleteOneTurn(turn int, c distributorChannels) {
	event := TurnComplete{}
	event.CompletedTurns = turn

	c.events <- event
}

func (w *World) WritePgm(turn int, c distributorChannels) {
	c.ioCommand <- ioOutput
	c.ioFilename <- w.imageName2(turn)
	for i := 0; i < w.Height; i++ {
		for j := 0; j < w.Width; j++ {
			if w.Grid1.Alive(i, j) {
				c.ioOutput <- 255
			} else {
				c.ioOutput <- 0
			}
		}
	}
}

func (w *World) DiffGrid(y, x int) bool {
	return w.Grid1.Status[y][x] != w.Grid2.Status[y][x]
}

func (w *World) SendCellFlipped(turn int, c distributorChannels) {
	for y := 0; y < w.Height; y++ {
		for x := 0; x < w.Width; x++ {
			if w.DiffGrid(y, x) {
				event := CellFlipped{}
				event.CompletedTurns = turn
				event.Cell = util.Cell{X: x, Y: y}
				c.events <- event
			}
		}
	}
}

func (w *World) Quit(turn int, c distributorChannels) {
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- StateChange{turn, Quitting}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {
	t := time.Now()
	// TODO: Create a 2D slice to store the world.
	world := NewWorld(p.ImageWidth, p.ImageHeight, p.Threads)
	turn := 0
	world.initCell(c)

	world.SendCellFlipped(turn, c)

	// TODO: Execute all turns of the Game of Life.
	ticker := time.NewTicker(2 * time.Second)
	for turn < p.Turns {
		//world.DebugLog(turn)
		select {
		case op := <-c.keyPresses:
			if op == 's' {
				world.WritePgm(turn, c)
			} else if op == 'q' {
				world.WritePgm(turn, c)
				goto quit
			} else if op == 'p' {
				fmt.Printf("turn=%v\n", turn)
				for {
					tmp := <-c.keyPresses
					if tmp == 'p' {
						fmt.Printf("Continuing!\n")
						break
					}
				}
			}
		case <-ticker.C:
			world.SendAliveCount(turn, c)
		default:
			turn++
			world.NextStep(0, world.Width)
			world.SendCellFlipped(turn, c)
			world.SendCompleteOneTurn(turn, c)
			//time.Sleep(time.Millisecond * 5000)
		}
	}
	//world.DebugLog(turn)

	// TODO: Report the final state using FinalTurnCompleteEvent.
	world.SendFinal(turn, c)

	world.WritePgm(turn, c)

	fmt.Printf("time cost: %v\n", time.Since(t))
quit:
	// Make sure that the Io has finished any output before exiting.
	world.Quit(turn, c)

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
