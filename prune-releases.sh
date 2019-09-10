#!/usr/bin/env bash
set -e

source _get_helm.sh

if [ -n "${DEBUG}" ]; then
    set -x
fi

function usage()
{
    echo "$0"
    echo "    -h --help"
    echo '    --older-than="4 weeks ago"                  <GNU date formatted date string>'
    echo '    --helm-release-filter="^feature-.+-web$"    <Helm release regex filter>'
    echo '    --namespace-filter="^feature-.+"            <Namespace regex filter>'
    echo '    --dry-run'
    echo ""
    echo "Example: $0 --older-than=\"2 weeks ago\" --helm-release-filter=\"^feature-.+-web$\" --namespace-filter=\"^feature-.+\""
}

function check_helm() {
    if [ ${HELM_VERSION} == "" ]; then
        HELM_VERSION="v2.13.1"
    fi

    _get_helm "${HELM_VERSION}"
    _set_helm_version "${HELM_VERSION}"

    echo "Set the Helm Version to ${HELM_VERSION}"
}

older_than_filter=""
release_filter=""
namespace_filter=""
dry_run=""

while [ "$1" != "" ]; do
    PARAM=`echo $1 | awk -F= '{print $1}'`
    VALUE=`echo $1 | awk -F= '{print $2}'`
    case $PARAM in
        -h | --help)
            usage
            exit
            ;;
        --older-than)
            older_than_filter="$VALUE"
            ;;
        --helm-release-filter)
            release_filter="$VALUE"
            ;;
        --namespace-filter)
            namespace_filter="$VALUE"
            ;;
        --dry-run)
            dry_run="TRUE"
            ;;
        *)
            echo "ERROR: unknown parameter \"$PARAM\""
            usage
            exit 1
            ;;
    esac
    shift
done

if [ -z "$older_than_filter" -o -z "$release_filter" -o -z "$namespace_filter" ]; then
    usage
    exit 1
fi

older_than_filter_s=$(date --date="$older_than_filter" +%s)

if [ -n "$dry_run" ]; then
    echo "Dry run mode. Nothing will be purged."
fi

counter=0
counter_purge=0
while read release_line ; do
    counter=$((counter+1))
    release_date=""
    release_date_s=0
    release_name=""
    release_namespace=""

    # skip processing for header line
    if [[ "$counter" -eq 1 ]]; then continue; fi

    # parse each release line
    release_date=$(echo "$release_line" | awk -F'\t' '{ print $3 }')
    release_date_s=$(date --date="$release_date" +%s)
    release_name=$(echo "$release_line" | awk -F'\t' '{ print $1 }' | tr -d " ")
    release_namespace=$(echo "$release_line" | awk -F'\t' '{ print $7 }' | tr -d " ")

    if [[ "$release_date_s" -le "$older_than_filter_s" ]]; then
        # Confirm release and namespace values
        if ! [[ "$release_name" =~ $release_filter ]]; then
            echo "Error: Release: '$release_name' does not match '$release_filter'"
            exit 1
        fi
        if ! [[ "$release_namespace" =~ $namespace_filter ]]; then
            echo "Error: Release: '$release_name' namespace: '$release_namespace' does not match '$namespace_filter'"
            exit 1
        fi
        echo "$release_line"
        counter_purge=$((counter_purge+1))
        [ -z "$dry_run" ] && helm delete --purge $release_name
        [ -z "$dry_run" ] && kubectl delete ns $release_namespace
    fi
done < <(helm ls "$release_filter")
[ $counter_purge -gt 0 ] || echo "No stale Helm charts found."
