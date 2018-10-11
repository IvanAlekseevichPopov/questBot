package main

import "testing"

func TestLoadConfig(t *testing.T) {
	loadConfig("config.yml")
}

func TestLoadStory(t *testing.T) {
	loadStory("content/story.json")
	checkStory()
}
