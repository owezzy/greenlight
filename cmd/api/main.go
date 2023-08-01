package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
	"greenlight.owezzy.net/internal/data"
	"greenlight.owezzy.net/internal/data/jsonlog"
)

// Declare a string containing the application version number.
const version = "1.0.0"

// Define a config struct to hold all the configuration settings for our application.
type config struct {
	port int
	env  string
	db   struct {
		dsn          string
		maxOpenConns int
		maxIdleConns int
		maxIdleTime  string
	}
	// Add a new limiter struct containing fields for the requests-per-second and burst
	// values, and a boolean field which we can use to enable/disable rate limiting
	// altogether.
	limiter struct {
		rps     float64
		burst   int
		enabled bool
	}
}

// Define an application struct to hold the dependencies for our HTTP handlers, helpers, and middleware.
type application struct {
	config config
	logger *jsonlog.Logger
	models data.Models
}

func main() {
	var cfg config

	flag.IntVar(&cfg.port, "port", 4000, "API server port")
	flag.StringVar(&cfg.env, "env", "development", "Environment (development|staging|production)")

	// Read the DSN value from the db-dsn command-line flag into the config struct. We
	// default to using our development DSN if no flag is provided.

	flag.StringVar(&cfg.db.dsn, "db-dsn", os.Getenv("GREENLIGHT_DB_DSN"), "PostgreSQL DSN")

	// Read the connection pool settings from command-line flags into the config struct.
	// Notice the default values that we're using?
	flag.IntVar(&cfg.db.maxOpenConns, "db-max-open-conns", 25, "PostgreSQL max open connections")
	flag.IntVar(&cfg.db.maxIdleConns, "db-max-idle-conns", 25, "PostgreSQL max idle connections")
	flag.StringVar(&cfg.db.maxIdleTime, "db-max-idle-time", "15m", "PostgreSQL max connection idle time")

	// Create command line flags to read the setting values into the config struct.
	// Notice that we use true as the default for the 'enabled' setting?
	flag.Float64Var(&cfg.limiter.rps, "limiter-rps", 2, "Rate limiter maximum requests per second")
	flag.IntVar(&cfg.limiter.burst, "limiter-burst", 4, "Rate limiter maximum burst")
	flag.BoolVar(&cfg.limiter.enabled, "limiter-enabled", true, "Enable rate limiter")

	flag.Parse()

	// Initialize a new jsonlog.Logger which writes any messages *at or above* the INFO
	// severity level to the standard out stream.
	logger := jsonlog.New(os.Stdout, jsonlog.LevelInfo)
	db, err := openDB(cfg)
	if err != nil {
		logger.PrintFatal(err, nil)
	}

	defer db.Close()
	// Also log a message to say that the connection pool has been successfully // established.
	logger.PrintInfo("database connection pool established", nil)

	// Declare an instance of the application struct, containing the config struct and  the logger.
	app := &application{
		config: cfg,
		logger: logger,
		models: data.NewModels(db),
	}
	// Declare a new servemux and add a /v1/healthcheck route which dispatches requests // to the healthcheckHandler method (which we will create in a moment).
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthcheck", app.healthcheckHandler)
	// Declare a HTTP server with some sensible timeout settings, which listens on the // port provided in the config struct and uses the servemux we created above as the // handler.
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.port),
		Handler:      app.routes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Again, we use the PrintInfo() method to write a "starting server" message at the // INFO level. But this time we pass a map containing additional properties (the
	// operating environment and server address) as the final parameter.
	logger.PrintInfo("starting server", map[string]string{
		"addr": srv.Addr,
		"env":  cfg.env,
	})
	// Call app.serve() to start the server.
	err = app.serve()
	if err != nil {
		logger.PrintFatal(err, nil)
	}
}

// The openDB() function returns a sql.DB connection pool.
func openDB(cfg config) (*sql.DB, error) {
	// Use sql.Open() to create an empty connection pool, using the DSN from the config // struct.
	db, err := sql.Open("postgres", cfg.db.dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(cfg.db.maxOpenConns)
	db.SetMaxIdleConns(cfg.db.maxIdleConns)
	// Use the time.ParseDuration() function to convert the idle timeout duration string // to a time.Duration type.
	duration, err := time.ParseDuration(cfg.db.maxIdleTime)
	// Set the maximum idle timeout.
	db.SetConnMaxIdleTime(duration)
	if err != nil {
		return nil, err
	}
	// Create a context with a 5-second timeout deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Use PingContext() to establish a new connection to the database, passing in the // context we created above as a parameter. If the connection couldn't be
	// established successfully within the 5 second deadline, then this will return an // error.
	err = db.PingContext(ctx)
	if err != nil {
		return nil, err
	}
	// Return the sql.DB connection pool.
	return db, nil
}
