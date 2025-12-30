#!/bin/bash

set -e

echo "Installing Go dependencies..."
go mod download
go mod tidy

export PGPASSWORD="val1dat0r"

until psql -h localhost -p 5432 -U validator -d postgres -c '\q' 2>/dev/null; do
    echo "Waiting for PostgreSQL to be ready..."
    sleep 1
done

psql -h localhost -p 5432 -U validator -d postgres -c "SELECT 1 FROM pg_database WHERE datname = 'project-sem-1'" | grep -q 1 || \
    psql -h localhost -p 5432 -U validator -d postgres -c "CREATE DATABASE \"project-sem-1\""

