-- name: CreateUser :one
INSERT INTO users (email) VALUES ($1) RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email)=lower($1::text);

-- name: CreateConnection :one
INSERT INTO connections (user_id,provider,external_user_id,encrypted_credentials)
VALUES ($1,$2,$3,$4) RETURNING *;

-- name: GetConnectionForUser :one
SELECT * FROM connections WHERE id=$1 AND user_id=$2;

-- name: UpdateConnectionForUser :one
UPDATE connections SET encrypted_credentials=$3,status=$4,updated_at=now()
WHERE id=$1 AND user_id=$2 RETURNING *;
