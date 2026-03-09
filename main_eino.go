package main

import "os"

func main() {
	if os.Getenv("OPENAI_API_KEY") == "" {
		println("❌ 请先设置 API Key:")
		println("   $env:OPENAI_API_KEY = \"your-api-key\"")
		return
	}
	StartWebChat()
}
