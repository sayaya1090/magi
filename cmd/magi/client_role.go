package main

import "github.com/sayaya1090/magi/internal/app"

func clientSystem(role string) string {
	if system := app.ClientRoleSystem(role); system != "" {
		return system
	}
	return systemPrompt
}
