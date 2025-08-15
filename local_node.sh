#!/bin/bash

CHAINID="${CHAIN_ID:-9001}"
MONIKER="localtestnet"
KEYRING="test"
KEYALGO="eth_secp256k1"

LOGLEVEL="info"
CHAINDIR="$HOME/.evmd"

BASEFEE=10000000

CONFIG_TOML=$CHAINDIR/config/config.toml
APP_TOML=$CHAINDIR/config/app.toml
GENESIS=$CHAINDIR/config/genesis.json
TMP_GENESIS=$CHAINDIR/config/tmp_genesis.json

# validate dependencies are installed
command -v jq >/dev/null 2>&1 || {
  echo >&2 "jq not installed. More info: https://stedolan.github.io/jq/download/"
  exit 1
}

set -e

# ------------- Flags -------------
install=true
overwrite=""
BUILD_FOR_DEBUG=false
ADDITIONAL_USERS=0
MNEMONIC_FILE=""   # default later to $CHAINDIR/mnemonics.yaml

usage() {
  cat <<EOF
Usage: $0 [options]

Options:
  -y                       Overwrite existing chain data without prompt
  -n                       Do not overwrite existing chain data
  --no-install             Skip 'make install'
  --remote-debugging       Build with nooptimization,nostrip
  --additional-users N     Create N extra users: dev4, dev5, ...
  --mnemonic-file PATH     Where to write mnemonics YAML (default: \$HOME/.evmd/mnemonics.yaml)
EOF
}

while [[ $# -gt 0 ]]; do
  key="$1"
  case $key in
    -y)
      echo "Flag -y passed -> Overwriting the previous chain data."
      overwrite="y"; shift
      ;;
    -n)
      echo "Flag -n passed -> Not overwriting the previous chain data."
      overwrite="n"; shift
      ;;
    --no-install)
      echo "Flag --no-install passed -> Skipping installation of the evmd binary."
      install=false; shift
      ;;
    --remote-debugging)
      echo "Flag --remote-debugging passed -> Building with remote debugging options."
      BUILD_FOR_DEBUG=true; shift
      ;;
    --additional-users)
      if [[ -z "${2:-}" || "$2" =~ ^- ]]; then
        echo "Error: --additional-users requires a number."; usage; exit 1
      fi
      ADDITIONAL_USERS="$2"; shift 2
      ;;
    --mnemonic-file)
      if [[ -z "${2:-}" || "$2" =~ ^- ]]; then
        echo "Error: --mnemonic-file requires a path."; usage; exit 1
      fi
      MNEMONIC_FILE="$2"; shift 2
      ;;
    -h|--help)
      usage; exit 0
      ;;
    *)
      echo "Unknown flag passed: $key -> Aborting"; usage; exit 1
      ;;
  esac
done

if [[ $install == true ]]; then
  if [[ $BUILD_FOR_DEBUG == true ]]; then
    make install COSMOS_BUILD_OPTIONS=nooptimization,nostrip
  else
    make install
  fi
fi

# Prompt if -y was not passed to original invocation
if [[ $overwrite = "" ]]; then
  if [ -d "$CHAINDIR" ]; then
    printf "\nAn existing folder at '%s' was found. You can choose to delete this folder and start a new local node with new keys from genesis. When declined, the existing local node is started. \n" "$CHAINDIR"
    echo "Overwrite the existing configuration and start a new local node? [y/n]"
    read -r overwrite
  else
    overwrite="y"
  fi
fi

# ---------- YAML writer ----------
write_mnemonics_yaml() {
  local file_path="$1"; shift
  local -a mns=("$@")
  mkdir -p "$(dirname "$file_path")"
  {
    echo "mnemonics:"
    for m in "${mns[@]}"; do
      printf '  - "%s"\n' "$m"
    done
  } > "$file_path"
  echo "Wrote mnemonics to $file_path"
}

# ---------- Add funded account ----------
add_genesis_funds() {
  local keyname="$1"
  evmd genesis add-genesis-account "$keyname" 1000000000000000000000atest --keyring-backend "$KEYRING" --home "$CHAINDIR"
}

# Setup local node if overwrite is set to Yes, otherwise skip setup
if [[ $overwrite == "y" || $overwrite == "Y" ]]; then
  rm -rf "$CHAINDIR"

  evmd config set client chain-id "$CHAINID" --home "$CHAINDIR"
  evmd config set client keyring-backend "$KEYRING" --home "$CHAINDIR"

  # ---------------- Validator key ----------------
  VAL_KEY="mykey"
  VAL_MNEMONIC="gesture inject test cycle original hollow east ridge hen combine junk child bacon zero hope comfort vacuum milk pitch cage oppose unhappy lunar seat"
  echo "$VAL_MNEMONIC" | evmd keys add "$VAL_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$CHAINDIR"

  # ---------------- Default dev keys ----------------
  USER1_KEY="dev0"
  USER2_KEY="dev1"
  USER3_KEY="dev2"
  USER4_KEY="dev3"

  default_mnemonics=(
    "copper push brief egg scan entry inform record adjust fossil boss egg comic alien upon aspect dry avoid interest fury window hint race symptom" # dev0
    "maximum display century economy unlock van census kite error heart snow filter midnight usage egg venture cash kick motor survey drastic edge muffin visual" # dev1
    "will wear settle write dance topic tape sea glory hotel oppose rebel client problem era video gossip glide during yard balance cancel file rose" # dev2
    "doll midnight silk carpet brush boring pluck office gown inquiry duck chief aim exit gain never tennis crime fragile ship cloud surface exotic patch" # dev3
  )

  # Import default dev keys
  echo "${default_mnemonics[0]}" | evmd keys add "$USER1_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$CHAINDIR"
  echo "${default_mnemonics[1]}" | evmd keys add "$USER2_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$CHAINDIR"
  echo "${default_mnemonics[2]}" | evmd keys add "$USER3_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$CHAINDIR"
  echo "${default_mnemonics[3]}" | evmd keys add "$USER4_KEY" --recover --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$CHAINDIR"

  echo "$VAL_MNEMONIC" | evmd init $MONIKER -o --chain-id "$CHAINID" --home "$CHAINDIR" --recover

  # ---------- Genesis customizations ----------
  jq '.app_state["staking"]["params"]["bond_denom"]="atest"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["gov"]["deposit_params"]["min_deposit"][0]["denom"]="atest"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["gov"]["params"]["min_deposit"][0]["denom"]="atest"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["gov"]["params"]["expedited_min_deposit"][0]["denom"]="atest"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["evm"]["params"]["evm_denom"]="atest"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state["mint"]["params"]["mint_denom"]="atest"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state["bank"]["denom_metadata"]=[{"description":"The native staking token for evmd.","denom_units":[{"denom":"atest","exponent":0,"aliases":["attotest"]},{"denom":"test","exponent":18,"aliases":[]}],"base":"atest","display":"test","name":"Test Token","symbol":"TEST","uri":"","uri_hash":""}]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state["evm"]["params"]["active_static_precompiles"]=["0x0000000000000000000000000000000000000100","0x0000000000000000000000000000000000000400","0x0000000000000000000000000000000000000800","0x0000000000000000000000000000000000000801","0x0000000000000000000000000000000000000802","0x0000000000000000000000000000000000000803","0x0000000000000000000000000000000000000804","0x0000000000000000000000000000000000000805", "0x0000000000000000000000000000000000000806", "0x0000000000000000000000000000000000000807"]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state["evm"]["params"]["evm_denom"]="atest"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.app_state.erc20.native_precompiles=["0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"
  jq '.app_state.erc20.token_pairs=[{contract_owner:1,erc20_address:"0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE",denom:"atest",enabled:true}]' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  jq '.consensus.params.block.max_gas="10000000"' "$GENESIS" >"$TMP_GENESIS" && mv "$TMP_GENESIS" "$GENESIS"

  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' 's/timeout_propose = "3s"/timeout_propose = "2s"/g' "$CONFIG_TOML"
    sed -i '' 's/timeout_propose_delta = "500ms"/timeout_propose_delta = "200ms"/g' "$CONFIG_TOML"
    sed -i '' 's/timeout_prevote = "1s"/timeout_prevote = "500ms"/g' "$CONFIG_TOML"
    sed -i '' 's/timeout_prevote_delta = "500ms"/timeout_prevote_delta = "200ms"/g' "$CONFIG_TOML"
    sed -i '' 's/timeout_precommit = "1s"/timeout_precommit = "500ms"/g' "$CONFIG_TOML"
    sed -i '' 's/timeout_precommit_delta = "500ms"/timeout_precommit_delta = "200ms"/g' "$CONFIG_TOML"
    sed -i '' 's/timeout_commit = "5s"/timeout_commit = "1s"/g' "$CONFIG_TOML"
    sed -i '' 's/timeout_broadcast_tx_commit = "10s"/timeout_broadcast_tx_commit = "5s"/g' "$CONFIG_TOML"
  else
    sed -i 's/timeout_propose = "3s"/timeout_propose = "2s"/g' "$CONFIG_TOML"
    sed -i 's/timeout_propose_delta = "500ms"/timeout_propose_delta = "200ms"/g' "$CONFIG_TOML"
    sed -i 's/timeout_prevote = "1s"/timeout_prevote = "500ms"/g' "$CONFIG_TOML"
    sed -i 's/timeout_prevote_delta = "500ms"/timeout_prevote_delta = "200ms"/g' "$CONFIG_TOML"
    sed -i 's/timeout_precommit = "1s"/timeout_precommit = "500ms"/g' "$CONFIG_TOML"
    sed -i 's/timeout_precommit_delta = "500ms"/timeout_precommit_delta = "200ms"/g' "$CONFIG_TOML"
    sed -i 's/timeout_commit = "5s"/timeout_commit = "1s"/g' "$CONFIG_TOML"
    sed -i 's/timeout_broadcast_tx_commit = "10s"/timeout_broadcast_tx_commit = "5s"/g' "$CONFIG_TOML"
  fi

  # enable prometheus metrics and all APIs for dev node
  if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' 's/prometheus = false/prometheus = true/' "$CONFIG_TOML"
    sed -i '' 's/prometheus-retention-time = 0/prometheus-retention-time  = 1000000000000/g' "$APP_TOML"
    sed -i '' 's/enabled = false/enabled = true/g' "$APP_TOML"
    sed -i '' 's/enable = false/enable = true/g' "$APP_TOML"
  else
    sed -i 's/prometheus = false/prometheus = true/' "$CONFIG_TOML"
    sed -i 's/prometheus-retention-time  = "0"/prometheus-retention-time  = "1000000000000"/g' "$APP_TOML"
    sed -i 's/enabled = false/enabled = true/g' "$APP_TOML"
    sed -i 's/enable = false/enable = true/g' "$APP_TOML"
  fi

  # Change proposal periods
  sed -i.bak 's/"max_deposit_period": "172800s"/"max_deposit_period": "30s"/g' "$GENESIS"
  sed -i.bak 's/"voting_period": "172800s"/"voting_period": "30s"/g' "$GENESIS"
  sed -i.bak 's/"expedited_voting_period": "86400s"/"expedited_voting_period": "15s"/g' "$GENESIS"

  # pruning
  sed -i.bak 's/pruning = "default"/pruning = "custom"/g' "$APP_TOML"
  sed -i.bak 's/pruning-keep-recent = "0"/pruning-keep-recent = "100"/g' "$APP_TOML"
  sed -i.bak 's/pruning-interval = "0"/pruning-interval = "10"/g' "$APP_TOML"

  # Allocate genesis accounts for validator and default users
  evmd genesis add-genesis-account "$VAL_KEY"   100000000000000000000000000atest --keyring-backend "$KEYRING" --home "$CHAINDIR"
  add_genesis_funds "$USER1_KEY"
  add_genesis_funds "$USER2_KEY"
  add_genesis_funds "$USER3_KEY"
  add_genesis_funds "$USER4_KEY"

  # --------- Generate additional users if requested ---------
  # final_mnemonics starts with defaults; we append generated ones next
  final_mnemonics=("${default_mnemonics[@]}")

  if [[ -z "$MNEMONIC_FILE" ]]; then
    MNEMONIC_FILE="$CHAINDIR/mnemonics.yaml"
  fi

  if [[ "$ADDITIONAL_USERS" -gt 0 ]]; then
    START_INDEX=4  # dev0..dev3 already exist
    for ((i=0; i<ADDITIONAL_USERS; i++)); do
      DEV_INDEX=$((START_INDEX + i))
      KEYNAME="dev${DEV_INDEX}"

		MNEMONIC_OUTPUT=$(evmd keys add "$KEYNAME" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$CHAINDIR" 2>&1)
		USER_MNEMONIC=$(echo "$MNEMONIC_OUTPUT" | grep -E '^[a-z]+ [a-z]+ [a-z]+ [a-z]+ [a-z]+ [a-z]+ [a-z]+ [a-z]+ [a-z]+ [a-z]+ [a-z]+ [a-z]+' | tail -1)
    if [[ -z "$USER_MNEMONIC" ]]; then
      echo "Failed to capture mnemonic for $KEYNAME"; exit 1
    fi

      final_mnemonics+=("$USER_MNEMONIC")
      add_genesis_funds "$KEYNAME"
      echo "Created $KEYNAME"
    done
  fi

  # --------- Finalize genesis ---------
  evmd genesis gentx "$VAL_KEY" 1000000000000000000000atest --gas-prices ${BASEFEE}atest --keyring-backend "$KEYRING" --chain-id "$CHAINID" --home "$CHAINDIR"
  evmd genesis collect-gentxs --home "$CHAINDIR"
  evmd genesis validate-genesis --home "$CHAINDIR"

  # --------- Write YAML with mnemonics (defaults + generated) ---------
  write_mnemonics_yaml "$MNEMONIC_FILE" "${final_mnemonics[@]}"

	if [[ $1 == "pending" ]]; then
		echo "pending mode is on, please wait for the first block committed."
	fi
fi

# Start the node
evmd start "$TRACE" \
	--log_level $LOGLEVEL \
	--minimum-gas-prices=0.0001atest \
	--home "$CHAINDIR" \
	--json-rpc.api eth,txpool,personal,net,debug,web3 \
	--chain-id "$CHAINID"
