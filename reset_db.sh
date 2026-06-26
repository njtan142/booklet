#!/bin/bash
echo "Resetting booklet database records (excluding users)..."

# Execute truncate via booklet-db container
docker exec -i booklet-db psql -U postgres -d booklet -c "TRUNCATE TABLE documents CASCADE;"

echo "Database reset complete!"
