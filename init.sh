#!/usr/bin/env bash
set -euo pipefail

# ── Module Template Initializer ────────────────────────────────────────────────
#
# Usage: ./init.sh <module_id> <display_name> <go_module_path>
#
# Example:
#   ./init.sh catalog "Product Catalog" "go.edgescale.dev/kernel-contrib/catalog"
#   ./init.sh billing "Billing & Payments" "github.com/myorg/billing"

if [ "$#" -lt 3 ]; then
    echo "Usage: ./init.sh <module_id> <display_name> <go_module_path>"
    echo ""
    echo "  module_id       Lowercase identifier (e.g., catalog, billing, inventory)"
    echo "  display_name    Human-readable name (e.g., \"Product Catalog\")"
    echo "  go_module_path  Full Go module path (e.g., go.edgescale.dev/kernel-contrib/catalog)"
    echo ""
    echo "Example:"
    echo "  ./init.sh catalog \"Product Catalog\" \"go.edgescale.dev/kernel-contrib/catalog\""
    exit 1
fi

MODULE_ID="$1"
DISPLAY_NAME="$2"
GO_MODULE_PATH="$3"

# Derive PascalCase name from module_id (e.g., "my_module" → "MyModule")
PASCAL_CASE=$(echo "$MODULE_ID" | sed -E 's/(^|_)([a-z])/\U\2/g')

# Derive camelCase name (e.g., "my_module" → "myModule")  
CAMEL_CASE=$(echo "$PASCAL_CASE" | sed -E 's/^([A-Z])/\L\1/')

echo "╔══════════════════════════════════════════════════════════════╗"
echo "║  Kernel Module Template Initializer                         ║"
echo "╠══════════════════════════════════════════════════════════════╣"
echo "║  Module ID:      $MODULE_ID"
echo "║  Display Name:   $DISPLAY_NAME"
echo "║  Go Module Path: $GO_MODULE_PATH"
echo "║  PascalCase:     $PASCAL_CASE"
echo "║  camelCase:      $CAMEL_CASE"
echo "╚══════════════════════════════════════════════════════════════╝"
echo ""

# ── Rename Go package ─────────────────────────────────────────────────────────
echo "→ Renaming package from 'mymodule' to '$MODULE_ID'..."

# Find all .go files and replace package/import references
find . -name "*.go" -not -path "./.git/*" | while read -r file; do
    # Replace package declaration
    sed -i '' "s/^package mymodule/package $MODULE_ID/" "$file"
    
    # Replace import references
    sed -i '' "s|go.edgescale.dev/kernel-contrib/mymodule|$GO_MODULE_PATH|g" "$file"
    
    # Replace mymodule. prefix in code references (test imports)
    sed -i '' "s/mymodule \"go.edgescale.dev\/kernel-contrib\/mymodule\"/$MODULE_ID \"${GO_MODULE_PATH//\//\\/}\"/g" "$file"
    sed -i '' "s/mymodule\./$MODULE_ID./g" "$file"
    
    # Replace string references
    sed -i '' "s/\"mymodule\"/\"$MODULE_ID\"/g" "$file"
    sed -i '' "s/mymodule\\./$MODULE_ID./g" "$file"
    sed -i '' "s/module_mymodule/module_$MODULE_ID/g" "$file"
done

# ── Replace display name ──────────────────────────────────────────────────────
echo "→ Setting display name to '$DISPLAY_NAME'..."

find . -name "*.go" -not -path "./.git/*" | while read -r file; do
    sed -i '' "s/My Module/$DISPLAY_NAME/g" "$file"
done

# ── Replace struct/interface names ─────────────────────────────────────────────
echo "→ Updating type names..."

find . -name "*.go" -not -path "./.git/*" | while read -r file; do
    sed -i '' "s/MyModuleReader/${PASCAL_CASE}Reader/g" "$file"
done

# ── Update go.mod ──────────────────────────────────────────────────────────────
echo "→ Updating go.mod..."
sed -i '' "s|go.edgescale.dev/kernel-contrib/mymodule|$GO_MODULE_PATH|g" go.mod

# ── Update SQL migrations table names ─────────────────────────────────────────
echo "→ Updating migration table name..."
find ./migrations -name "*.sql" | while read -r file; do
    sed -i '' "s/items/${MODULE_ID}s/g" "$file"
done

# ── Update README ──────────────────────────────────────────────────────────────  
echo "→ Updating README..."
sed -i '' "s/mymodule/$MODULE_ID/g" README.md
sed -i '' "s/My Module/$DISPLAY_NAME/g" README.md
sed -i '' "s/MyModule/$PASCAL_CASE/g" README.md

echo ""
echo "✅ Module initialized successfully!"
echo ""
echo "Next steps:"
echo "  1. Run 'go mod tidy' to resolve dependencies"
echo "  2. Run 'go test -v ./...' to verify everything compiles"
echo "  3. Edit models.go to define your domain models"
echo "  4. Edit service.go to implement your business logic"
echo "  5. Edit handlers.go to wire up your API endpoints"
echo ""

# Self-delete
rm -- "$0"
