#!bash

set -euxo pipefail

PROJ_NAME=DockerMirrorBox
CADATE=$(date "+%Y.%m.%d %H:%M")
CAID="$(hostname -f) ${CADATE}"
CN_CA="${PROJ_NAME} CA Root ${CAID}"
CN_CA=${CN_CA:0:64}

CA_KEY_FILE=${CA_KEY_FILE:-ca.key}
CA_CRT_FILE=${CA_CRT_FILE:-ca.crt}
CA_SRL_FILE=${CA_SRL_FILE:-ca.srl}

if [ ! -f "$CA_KEY_FILE" ] ; then
    openssl genrsa -des3 -passout pass:foobar -out ${CA_KEY_FILE} 4096
    rm -f ${CA_CRT_FILE}
fi

if [ ! -f "$CA_CRT_FILE" ] ; then
    openssl req -new -x509 -days 1300 -sha256 -key ${CA_KEY_FILE} -out ${CA_CRT_FILE} -passin pass:foobar -subj "/C=NL/ST=Noord Holland/L=Amsterdam/O=ME/OU=IT/CN=${CN_CA}" -extensions IA -config <(
cat <<-EOF
[req]
distinguished_name = dn
[dn]
[IA]
basicConstraints = critical,CA:TRUE
keyUsage = critical, digitalSignature, cRLSign, keyCertSign
subjectKeyIdentifier = hash
EOF
)
    rm -f ${CA_SRL_FILE}
fi

if [ ! -f "$CA_SRL_FILE" ] ; then
    echo 01 > ${CA_SRL_FILE}
fi
