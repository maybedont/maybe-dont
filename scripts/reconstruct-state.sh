#!/bin/bash
# Reconstruct .ai-test-state.json from an ai-results.json artifact and local test case files.
#
# Usage:
#   ./scripts/reconstruct-state.sh <ai-results.json> [output-file]
#
# This script is a recovery tool for when the CI state file was not persisted.
# It computes content hashes and policy hashes from local files (which must match
# what was used in the CI run), then maps results from ai-results.json into the
# state file format used by `maybe-dont test policies --incremental`.
#
# NOTE: Content hashes are SHA256 of raw file bytes — if local files differ from
# what CI used (e.g., you've made edits since the run), the hashes won't match
# and incremental mode will re-run those tests. Use `git stash` or check out the
# exact commit the CI run used before running this script.

set -euo pipefail

RESULTS_FILE="${1:?Usage: $0 <ai-results.json> [output-file]}"
OUTPUT_FILE="${2:-.ai-test-state.json}"

REPO_ROOT="$(git rev-parse --show-toplevel)"
SUITE_DIR="$REPO_ROOT/internal/config/defaults/tests"
CASES_DIR="$SUITE_DIR/cases"
DEFAULTS_DIR="$REPO_ROOT/internal/config/defaults"

if [ ! -f "$RESULTS_FILE" ]; then
  echo "Error: results file not found: $RESULTS_FILE" >&2
  exit 1
fi

if [ ! -d "$CASES_DIR" ]; then
  echo "Error: test cases directory not found: $CASES_DIR" >&2
  exit 1
fi

echo "Building case_id -> content_hash mapping..."

# Build a JSON object mapping case_id -> content_hash
# Each test case file can contain multiple case_ids (YAML array), all sharing the same content hash.
case_mapping="{}"
file_count=0
case_count=0

for file in "$CASES_DIR"/*.yaml; do
  [ -f "$file" ] || continue
  file_count=$((file_count + 1))

  # Compute content hash: SHA256 of raw file bytes, matching Go's ComputeContentHash()
  hash="sha256:$(shasum -a 256 "$file" | cut -d' ' -f1)"

  # Extract case_ids from YAML. Each case_id line looks like:
  #   case_id: ai-req-020
  #   case_id: "some-id"
  while IFS= read -r line; do
    # Strip the key prefix, surrounding quotes, and whitespace
    cid=$(echo "$line" | sed -E 's/^.*case_id:[[:space:]]*//' | sed 's/^["'"'"']//' | sed 's/["'"'"']$//' | xargs)
    [ -z "$cid" ] && continue
    case_mapping=$(echo "$case_mapping" | jq --arg cid "$cid" --arg hash "$hash" '. + {($cid): $hash}')
    case_count=$((case_count + 1))
  done < <(grep 'case_id:' "$file")
done

echo "  Found $case_count test cases across $file_count files"

echo "Computing policy hashes..."

# Compute sorted policy hashes matching runner.computePolicyHashes() order:
#   cel_request_rules, ai_request_rules, cel_response_rules, ai_response_rules
# Each path is resolved relative to suite.yaml's parent directory (the defaults dir).
policy_hashes="[]"
policy_count=0

for pf in \
  "$DEFAULTS_DIR/cel_request_rules.yaml" \
  "$DEFAULTS_DIR/ai_request_rules.yaml" \
  "$DEFAULTS_DIR/cel_response_rules.yaml" \
  "$DEFAULTS_DIR/ai_response_rules.yaml"; do
  if [ -f "$pf" ]; then
    hash="sha256:$(shasum -a 256 "$pf" | cut -d' ' -f1)"
    policy_hashes=$(echo "$policy_hashes" | jq --arg h "$hash" '. + [$h]')
    policy_count=$((policy_count + 1))
  else
    echo "  Warning: policy file not found: $pf" >&2
  fi
done

# Sort to match Go's sort.Strings(hashes)
policy_hashes=$(echo "$policy_hashes" | jq 'sort')
echo "  Hashed $policy_count policy files"

echo "Reading results from $RESULTS_FILE..."

# Extract bundle_id from suite.yaml
bundle_id=$(grep 'bundle_id:' "$SUITE_DIR/suite.yaml" | head -1 | sed 's/.*bundle_id:[[:space:]]*//' | xargs)
now=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Count models and results in the input
model_count=$(jq '.results_by_model | length' "$RESULTS_FILE")
total_results=$(jq '[.results_by_model[].results | length] | add // 0' "$RESULTS_FILE")
echo "  Found $total_results results across $model_count models"

# Build the state file using jq.
#
# Strategy: iterate results_by_model -> results, look up content_hash from case_mapping,
# and group by content_hash. For duplicate (content_hash, model_key) pairs, last wins
# (matching the runner's RecordResult behavior where later calls overwrite earlier ones).
#
# Skipped results are excluded since they weren't actually evaluated.
jq -n \
  --arg schema_version "v1" \
  --arg product_version "dev" \
  --arg suite_id "$bundle_id" \
  --arg now "$now" \
  --argjson case_mapping "$case_mapping" \
  --argjson policy_hashes "$policy_hashes" \
  --slurpfile results "$RESULTS_FILE" \
'
{
  schema_version: $schema_version,
  product_version: $product_version,
  suite_id: $suite_id,
  last_updated: $now,
  results: (
    # Flatten all results into (content_hash, case_id, model_key, result) tuples
    [
      $results[0].results_by_model[] |
      (.model.provider + ":" + .model.model) as $model_key |
      .results[] |
      select(.status != "skipped") |
      $case_mapping[.case_id] as $content_hash |
      select($content_hash != null) |
      {
        content_hash: $content_hash,
        case_id: .case_id,
        model_key: $model_key,
        status: .status,
        confidence: (.actual.confidence // 0),
        duration_ms: .elapsed_ms
      }
    ] |

    # Group by content_hash to build one CachedTestCase per file
    group_by(.content_hash) |
    map(
      .[0].content_hash as $hash |
      # For the case_id field, take the last one (matches runner behavior)
      (last | .case_id) as $last_case_id |
      {
        ($hash): {
          case_id: $last_case_id,
          policy_hashes: $policy_hashes,
          models: (
            # Group by model_key within this content_hash, last wins
            group_by(.model_key) |
            map(
              last |
              {
                (.model_key): {
                  status: .status,
                  confidence: .confidence,
                  last_run: $now,
                  duration_ms: .duration_ms
                }
              }
            ) | add // {}
          )
        }
      }
    ) | add // {}
  )
}
' > "$OUTPUT_FILE"

# Summary
entry_count=$(jq '.results | length' "$OUTPUT_FILE")
model_keys=$(jq -r '[.results[].models | keys[]] | unique | join(", ")' "$OUTPUT_FILE")
echo ""
echo "Wrote $OUTPUT_FILE"
echo "  State entries: $entry_count (one per test case file)"
echo "  Models: $model_keys"
echo "  Policy hashes: $(echo "$policy_hashes" | jq 'length')"
echo ""
echo "To verify, run:"
echo "  ./maybe-dont test policies --suite-dir internal/config/defaults/tests --engine ai --matrix --incremental --state-file $OUTPUT_FILE --summary-only"
