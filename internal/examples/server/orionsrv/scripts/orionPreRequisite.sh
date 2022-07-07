#!/usr/bin/env sh

# Copyright (c) AppDynamics, Inc., and its affiliates 2022
# All Rights Reserved.
# THIS IS UNPUBLISHED PROPRIETARY CODE OF APPDYNAMICS, INC.
#
# The copyright notice above does not evidence any actual or
# intended publication of such source code

set -o nounset
set -xe

readonly ME="$(basename "$0")"
readonly HERE=$(CDPATH='' cd "$(dirname "$0")" && pwd -P)

readonly BASE_URL="http://localhost:8084/json/v1"
readonly OPAMP_OPERATOR_QUALIFIED_API_PATH="/opamp:operator"
readonly SCHEMA_API_PATH="/metadata/definition"

readonly HTTP_STATUS_FILE="http_$$.status"

readonly ERR_HELP=1
readonly ERR_BAD_ARGS=2
readonly ERR_DEPS=3
readonly ERR_BAD_RESPONSE=4
readonly ERR_NETWORK=5

readonly CONTENT_TYPE_HEADER="Content-Type: application/json"
readonly PRINCIPAL_ID_HEADER="appd-pid: dXNlcg=="
readonly PRINCIPAL_TYPE_HEADER="appd-pty: c2VydmljZQ=="
readonly LAYER_TYPE_HEADER="layer-type: solution"
readonly LAYER_ID_HEADER="layer-id: opamp"
readonly AUTHORIZATION_HEADER="Authorization: Bearer http"
readonly OPAMP_OPERATOR_JSON="{
    \"apiVersion\":\"opentelemetry.io/v1alpha1\",
    \"kind\":\"OpenTelemetryCollector\",
    \"metadata\":{
        \"name\":\"test5\"
    },
    \"spec\":{
        \"config\":\"|\n    receivers:\n      otlp:\n        protocols:\n          grpc:\n          http:\n    processors:\n\n    exporters:\n      logging:\n\n    service:\n      pipelines:\n        traces:\n          receivers: [otlp]\n          processors: []\n          exporters: [logging]\"
    }
}"

# Switch to the script's directory.
cd "${HERE}" || exit

###################################################################################################################
#                                   USAGE AND ERROR HANDLING FUNCTIONS                                            #
###################################################################################################################

# Bare command usage function.
_usage() {
  echo "Usage: $ME [OPTIONS...]"
  echo "  -h, --help, help                Print this help"
  echo
  echo "  verifySchema                    Verify OPAMP operator metadata definition"
  echo
  echo "  createObject                    Create OPAMP operator object"
  echo
  echo "  getAllObjects                   Get ALL OPAMP operator objects"
}

# Prints an error message with an 'ERROR' prefix to stderr.
#
# Args:
#   $1 - error message.
error_msg() {
  echo "ERROR: $1" >&2
}

# Prints an informational message to stdout.
#
# Args:
#   $1 - message
info_msg() {
  echo "INFO: $1"
}

# Prints the command usage followed by an exit.
#
# Args:
#   $1 (optional) - exit code to use.
exit_with_usage() {
  _usage >&2
  if [ $# -gt 0 ]; then
    exit "$1"
  else
    exit "${ERR_HELP}"
  fi
}

# Prints an error message followed by an exit.
#
# Args:
#   $1 - error message.
#   $2 - exit code to use.
exit_with_error() {
  error_msg "$1"
  exit "$2"
}

# Prints an error message followed by an usage and exit with `ERR_BAD_ARGS`.
#
# Args:
#   $1 - error message.
exit_bad_args() {
  error_msg "$1"
  exit_with_usage ${ERR_BAD_ARGS}
}

# Removes temporary file during exit or interrupt.
cleanup() {
  rm -f "${HTTP_STATUS_FILE}"
}
trap cleanup EXIT TERM INT

###################################################################################################################
#                                              HELPER FUNCTIONS                                                   #
###################################################################################################################

# Checks dependencies required by this script.
# Unmet dependencies result in exit with `ERR_DEPS`.
check_dependencies() {
  if ! command -v curl >/dev/null 2>&1; then
    exit_with_error "curl command unavailable" ${ERR_DEPS}
  fi
}

# Runs `curl` command to execute the GET query. Network or HTTP failure
# results in exit with `ERR_NETWORK` and `ERR_BAD_RESPONSE` respectively.
#
# Args:
#   $1 - http URL.
do_curl_get() {
  if ! curl -qL -X GET -w "%{http_code}" "$1" \
    -H "${PRINCIPAL_ID_HEADER}" \
    -H "${PRINCIPAL_TYPE_HEADER}" \
    -H "${LAYER_TYPE_HEADER}" \
    -H "${LAYER_ID_HEADER}" \
    -H "${AUTHORIZATION_HEADER}" -eq 200; then
    info_msg "successfully queried: " "$1"
  else
    exit_with_error "failed to query Orion service" "${ERR_NETWORK}"
  fi
}

# Runs `curl` command to execute the PUT query. Network or HTTP failure
# results in exit with `ERR_NETWORK` and `ERR_BAD_RESPONSE` respectively.
#
# Args:
#   $1 - http URL.
do_curl_put() {
  if ! curl -qL -X PUT -w "%{http_code}" "$1" \
    -H "${CONTENT_TYPE_HEADER}" \
    -H "${PRINCIPAL_ID_HEADER}" \
    -H "${PRINCIPAL_TYPE_HEADER}" \
    -H "${LAYER_TYPE_HEADER}" \
    -H "${LAYER_ID_HEADER}" \
    -H "${AUTHORIZATION_HEADER}" -d "$2" -eq 200; then
    info_msg "successfully posted: " "$1"
  else
    exit_with_error "failed to post to Orion service" "${ERR_NETWORK}"
  fi
}

###################################################################################################################
#                                             COMMAND FUNCTIONS                                                   #
###################################################################################################################

verifySchema() {
  do_curl_get "${BASE_URL}""${OPAMP_OPERATOR_QUALIFIED_API_PATH}""${SCHEMA_API_PATH}"
}

createObject() {
  do_curl_put "${BASE_URL}""${OPAMP_OPERATOR_QUALIFIED_API_PATH}" "${OPAMP_OPERATOR_JSON}"
}

getAllObjects() {
  do_curl_get "${BASE_URL}""${OPAMP_OPERATOR_QUALIFIED_API_PATH}"
}

###################################################################################################################
#                                             MAIN ENTRYPOINT                                                     #
###################################################################################################################

main() {
  if [ $# -le 0 ]; then
    exit_with_usage
  fi

  # Let's ensure we have everything we need.
  check_dependencies

  while [ $# -gt 0 ]; do
    case "$1" in
    verifySchema)
      shift
      verifySchema "$@"
      exit $?
      ;;
    createObject)
      shift
      createObject "$@"
      exit $?
      ;;
    getAllObjects)
      shift
      getAllObjects "$@"
      exit $?
      ;;
    -h | --help | help)
      exit_with_usage
      ;;
    *)
      exit_bad_args "unknown command: $1"
      ;;
    esac
    shift
  done
}

main "$@"
