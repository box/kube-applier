package sysutil

import (
	"log"
	"os"
	"strconv"
)

func GetRequiredEnvString(key string) string {
	val := os.Getenv(key)
	if len(val) == 0 {
		log.Fatalf("Error: Missing environment variable %v", key)
	}
	return val
}

func GetRequiredEnvInt(key string) int {
	stringVal := GetRequiredEnvString(key)
	intVal, err := strconv.Atoi(stringVal)
	if err != nil {
		log.Fatalf("Error converting environment variable %s to int: %v", stringVal, err)
	}
	return intVal
}

func GetEnvIntOrDefault(key string, def int) int {
	if env := os.Getenv(key); env != "" {
		val, err := strconv.Atoi(env)
		if err != nil {
			log.Printf("Invalid value for %v: using default: %v", key, def)
			return def
		}
		return val
	}
	return def
}

func GetEnvStringOrDefault(key, def string) string {
	if env := os.Getenv(key); env != "" {
		return env
	}
	return def
}
