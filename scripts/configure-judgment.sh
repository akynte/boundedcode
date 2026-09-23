#!/usr/bin/env bash
# Configure the required TypeSafe Jev decision plane for the local install.
set -euo pipefail
set +x

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
data_dir="${BC_DATA:-${HOME}/.local/share/boundedcode}"
export BC_DATA="${data_dir}"
config_dir="${data_dir}/config"
judgment_file="${config_dir}/judgment.yaml"
secret_dir="${HOME}/.config/boundedcode"
secret_file="${secret_dir}/jev.env"
bashrc="${HOME}/.bashrc"

if [[ ! -f "${config_dir}/bcode.yaml" ]]; then
  echo "No BoundedCode config at ${config_dir}/bcode.yaml; run bcode config init first." >&2
  exit 1
fi

if command -v bcode >/dev/null 2>&1; then
  bcode_cmd="$(command -v bcode)"
elif [[ -x "${repo_root}/bin/bcode" ]]; then
  bcode_cmd="${repo_root}/bin/bcode"
else
  echo "bcode is not installed or built; run make build first." >&2
  exit 1
fi

mkdir -p "${secret_dir}"
chmod 700 "${secret_dir}"
umask 077
secret_tmp="$(mktemp "${secret_file}.XXXXXX")"
doctor_tmp="$(mktemp)"
trap 'rm -f "${secret_tmp}" "${config_tmp:-}" "${doctor_tmp}"' EXIT

printf 'TypeSafe Jev API key (input hidden): ' >&2
if ! IFS= read -r -s TYPESAFE_API_KEY </dev/tty; then
  echo >&2
  echo "Could not read the API key from the terminal." >&2
  exit 1
fi
echo >&2
if [[ -z "${TYPESAFE_API_KEY}" ]]; then
  echo "The API key cannot be empty." >&2
  exit 1
fi
printf 'export TYPESAFE_API_KEY=%q\n' "${TYPESAFE_API_KEY}" >"${secret_tmp}"
chmod 600 "${secret_tmp}"
mv -f -- "${secret_tmp}" "${secret_file}"
unset TYPESAFE_API_KEY

touch "${bashrc}"
if ! grep -Fq '# >>> boundedcode Jev key >>>' "${bashrc}"; then
  cat >>"${bashrc}" <<'BASHRC'

# >>> boundedcode Jev key >>>
if [[ -r "$HOME/.config/boundedcode/jev.env" ]]; then
  case $- in
    *x*) set +x; . "$HOME/.config/boundedcode/jev.env"; set -x ;;
    *) . "$HOME/.config/boundedcode/jev.env" ;;
  esac
fi
# <<< boundedcode Jev key <<<
BASHRC
fi

if [[ -f "${judgment_file}" ]]; then
  backup="${judgment_file}.bak.$(date -u +%Y%m%dT%H%M%SZ)"
  cp -p -- "${judgment_file}" "${backup}"
  printf 'Previous judgment config backed up to %s\n' "${backup}"
fi
config_tmp="$(mktemp "${judgment_file}.XXXXXX")"
cat >"${config_tmp}" <<'YAML'
enabled: true
model: jev-1.13.0
api_key_env: TYPESAFE_API_KEY
redact: strict
min_confidence: 0.75
cache: true
YAML
chmod 600 "${config_tmp}"
mv -f -- "${config_tmp}" "${judgment_file}"

# Load it for this check as well as for future Bash terminals.
set +x
source "${secret_file}"
printf 'Configured judgment settings and private key file (%s, mode 600).\n' "${secret_file}"
doctor_status=0
"${bcode_cmd}" doctor >"${doctor_tmp}" 2>&1 || doctor_status=$?
cat "${doctor_tmp}"
if grep -Fq '[FAIL]' "${doctor_tmp}"; then
  echo "Doctor reported a failure; fix it before running a task." >&2
  exit 1
fi
if [[ "${doctor_status}" -ne 0 ]]; then
  echo "Doctor reported warnings; review each entry above. Host installs warn about the missing container boundary and network limits; profile and index warnings clear after those checks are completed. The judgment warning describes metadata sent to Jev." >&2
fi
echo "New Bash terminals will load the key automatically. Keep this private file out of backups or sync services you do not trust."
