package server

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/PurgeBot-net/database"
	"github.com/PurgeBot-net/interactions/config"
	"github.com/PurgeBot-net/interactions/internal/commands"
)

type Server struct {
	cfg       config.Config
	logger    *zap.Logger
	db        *database.Database
	redis     *redis.Client
	client    *bot.Client
	publicKey ed25519.PublicKey
	router    *commands.Router
}

func New(cfg config.Config, logger *zap.Logger, db *database.Database, redis *redis.Client) (*Server, error) {
	keyBytes, err := hex.DecodeString(cfg.PublicKey)
	if err != nil || len(keyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("DISCORD_PUBLIC_KEY must be a %d-byte hex string", ed25519.PublicKeySize)
	}

	client, err := disgo.New(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create discord client: %w", err)
	}

	s := &Server{
		cfg:       cfg,
		logger:    logger,
		db:        db,
		redis:     redis,
		client:    client,
		publicKey: ed25519.PublicKey(keyBytes),
	}
	s.router = commands.NewRouter(cfg, logger, db, redis, client)
	return s, nil
}

func (s *Server) Start(ctx context.Context) error {
	if err := s.router.RegisterCommands(ctx); err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /interactions", s.handleInteraction)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: s.cfg.Addr, Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background()) //nolint:errcheck
	}()

	s.logger.Info("starting interactions server", zap.String("addr", s.cfg.Addr))
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) handleInteraction(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !verifySignature(s.publicKey, r.Header.Get("X-Signature-Ed25519"), r.Header.Get("X-Signature-Timestamp"), body) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Discord ping/pong handshake
	var raw struct {
		Type discord.InteractionType `json:"type"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if raw.Type == discord.InteractionTypePing {
		respond(w, discord.InteractionResponse{Type: discord.InteractionResponseTypePong})
		return
	}

	interaction, err := discord.UnmarshalInteraction(body)
	if err != nil {
		s.logger.Error("unmarshal interaction", zap.Error(err))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.router.Handle(r.Context(), interaction, func(resp discord.InteractionResponse) {
		respond(w, resp)
	})
}

func respond(w http.ResponseWriter, resp discord.InteractionResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func verifySignature(key ed25519.PublicKey, sigHex, timestamp string, body []byte) bool {
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	msg := append([]byte(timestamp), body...)
	return ed25519.Verify(key, msg, sig)
}
