#!/bin/sh
set -e

# Benchmark credentials script
# Runs the base credentials script, then overrides the token for benchmark mode
# This uses test@dummy.org which matches the submission_user of uploaded files

# Run the base credentials script first
/bin/sh /scripts/make_sda_credentials.sh

echo "=== Benchmark: Overriding token for data ownership model ==="

# Generate a new token using test@dummy.org (matches submission_user)
# This user matches the files.submission_user column set during upload
python /scripts/sign_jwt.py test@dummy.org > "/shared/token"

# Create trusted issuers file for visa validation (new download service)
# The issuer and JKU must match what mockoidc.py uses when signing visas
cat > "/shared/trusted-issuers.json" <<'EOF'
[
  {
    "iss": "https://demo.example",
    "jku": "http://mockauth:8000/jwks"
  }
]
EOF

echo "Benchmark credentials ready (token user: test@dummy.org)"
