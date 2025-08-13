package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-payments/internal/api"
	"go-payments/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const dbFileName = "payments.db"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("запуск приложения...")

	db, err := storage.New()
	if err != nil {
		log.Fatalf("ошибка при инициализации storage: %v", err)
	}

	if err := db.Init(ctx); err != nil {
		log.Fatalf("ошибка при инициализации данных")
	}

	log.Println("инициализация базы данных прошла успешно")

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	appAPI := api.New(db)
	appAPI.RegisterRoutes(r)

	server := &http.Server{
		Addr: ":8080",
		Handler: r,
	}

	go func ()  {
		log.Println("сервер запущен на http://localhost:8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ошибка запуска сервера: %v", err)
		}
	} ()

	<-ctx.Done()

	log.Println("получен сигнал завершения, остановка сервера...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("ошибка при остановке сервера: %v", err)
	}
	log.Println("сервер остановлен")
}