/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

func f1() {
	fmt.Println("hello world f1")
}

func f2(ch chan int, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Println("-----------writing value to channel-------------------")

	ch <- 1
	fmt.Println("-----------writing Done-------------------")
	fmt.Println("hello world f2")
}

func f3(ch chan int, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Println("waiting to receive value from channelchannel")
	time.Sleep(4 * time.Second)
	a := <-ch
	fmt.Println("Read done########################")
	fmt.Println("received value from channel", a)
}

func f4() {
	fmt.Println("hello world f3")
}

func main() {
	sl := make([]int, 5)
	for i := 0; i < 5; i++ {
		sl = append(sl, rand.Int())
	}
	ch := make(chan int)
	var wg sync.WaitGroup
	wg.Add(1)
	go f2(ch, &wg)
	wg.Add(1)
	go f3(ch, &wg)
	time.Sleep(25 * time.Second)
	wg.Wait()

}
