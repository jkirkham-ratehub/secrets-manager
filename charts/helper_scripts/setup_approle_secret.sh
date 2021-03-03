#!/bin/bash

while getopts ":h:a" flag
do
    case "${flag}" in
        h) help="true";;
        a) apply="true";;
        \? )
            echo "Invalid Option: -$OPTARG" 1>&2
            exit 1
            ;;
    esac
done

IFS='' read -r -d '' USAGE <<'EOF'
Usage: $0 [-h]
   -h: help/usage
   -a: apply secret to kubernetes.
Note: all other settings are passed via environment variables:
   PROJECT: [Required!] The project name should be the same as the target namespace in Kubernetes.
   VAULT_ADDR: [Required!] The URL for the Vault service instance to be accessed.
   VAULT_TOKEN: [Required!] The Vault token for authenicating with Vault.  It must have a policy permitting access to the AppRole you will be working with.
   VAULT_CACERT: [Optional] The CA certificate which may be required if Vault https is using a self-signed certificate.
   APP_ROLE_NAME: [Optional] The name ofthe Vault AppRole.  If not specified it will be the same as PROJECT.
EOF

if test "$help" == "true"; then
    echo "$USAGE"
    exit 0
fi

declare -a required_env=("PROJECT" "VAULT_TOKEN" "VAULT_ADDR")
for env_var in ${required_env[@]}; do
    if [ ! -v $env_var ]; then
        echo "env variable [$env_var] must be set."
        echo "The following ${#required_env[@]} env variables are required: ${required_env[@]}"
        echo "$USAGE"
        exit 1
    fi
done

if test -z $APP_ROLE_NAME; then
    echo "setting APP_ROLE_NAME to PROJECT name: $PROJECT"
    export APP_ROLE_NAME="$PROJECT"
fi

export PROJECT_SECRET_ID=$(vault write -force auth/approle/role/${APP_ROLE_NAME}/secret-id -format=json | jq '.data.secret_id' | tr -d '"')
if test -z $PROJECT_SECRET_ID; then
    echo "Failed to set AppRole secret-id for $APP_ROLE_NAME."
    exit 1
fi
export PROJECT_ROLE_ID=$(vault read auth/approle/role/${APP_ROLE_NAME}/role-id -format=json | jq '.data.role_id' | tr -d '"')
if test -z $PROJECT_ROLE_ID; then
    echo "Failed to get AppRole role-id for $APP_ROLE_NAME."
    exit 1
fi

kubectl create secret generic "${APP_ROLE_NAME}-approle-secret" --from-literal role_id=$PROJECT_ROLE_ID  --from-literal secret_id=$PROJECT_SECRET_ID --namespace ${PROJECT} --dry-run=client --output=yaml > ./${PROJECT}-approle-secret.yaml
if test $? -ne 0; then
    echo "Failed to generate YAML for the ${APP_ROLE_NAME}-approle-secret Secret in the $PROJECT namespace."
    exit 1
fi

if test "$apply" == "true"; then
    kubectl apply -f ./${PROJECT}-approle-secret.yaml -n $PROJECT
    if test $? -ne 0; then
        exit 1
    fi
fi
