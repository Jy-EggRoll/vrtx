package main

import (
	"log"
	"os"
	"path/filepath"
)

func main() {
	outputDir := getOutputDir()

	// 清理并创建输出目录
	os.RemoveAll(outputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output dir: %v", err)
	}

	log.Printf("Vortex: %s", outputDir)

	log.Println("Extracting bookmarks...")
	extractBookmarks(outputDir)

	log.Println("Vortex complete!")
}

func getOutputDir() string {
	tempDir := os.Getenv("TEMP")
	if tempDir == "" {
		tempDir = os.Getenv("TMP")
	}
	if tempDir == "" {
		homeDir, _ := os.UserHomeDir()
		tempDir = filepath.Join(homeDir, "AppData", "Local", "Temp")
	}
	return filepath.Join(tempDir, "eggroll-vrtx")
}
