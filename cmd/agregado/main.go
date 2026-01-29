package main

import (
	"fmt"
	"log"

	"github.com/felipeafreitas/agregado/internal/config"
)

func main() {
	cgf, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config", err)
	}
	fmt.Printf("Config loaded: %+v\n", cgf)
}
