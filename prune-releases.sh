#!/usr/bin/env bash
set -e

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
    echo "Dry run mode. Nothing will be deleted."
fi

counter=0
counter_delete=0
while read release_line ; do
    counter=$((counter+1))
    release_date=""
    release_date_s=0
    release_name=""
    release_namespace=""

    # skip processing for header line
    if [[ "$counter" -eq 1 ]]; then continue; fi

    # parse each release line
    release_date=$(echo "$release_line" | awk -F'\t' '{ print $4 }')
    # some dates look like 2020-07-03 20:19:59.322202 -0500 -0500, and date doesn't like the second offset
    release_date=`echo $release_date | sed 's/ [-+][[:digit:]]\+$//g'`
    release_date=`echo $release_date | sed 's/ UTC$//g'`
    release_date_s=$(date --date="$release_date" +%s)
    release_name=$(echo "$release_line" | awk -F'\t' '{ print $1 }' | tr -d " ")
    release_namespace=$(echo "$release_line" | awk -F'\t' '{ print $2 }' | tr -d " ")

    if [[ "$release_date_s" -le "$older_than_filter_s" ]]; then
        # Confirm release and namespace values
        if ! [[ "$release_name" =~ $release_filter ]]; then
            continue
        fi
        if ! [[ "$release_namespace" =~ $namespace_filter ]]; then
            continue
        fi
        echo "$release_line"
        counter_delete=$((counter_delete+1))
        [ -z "$dry_run" ] && helm delete --namespace $release_namespace $release_name

        # Delete the namespace if there are no other helm releases in it
        if [ "$(helm list --namespace $release_namespace --output json | jq ". | length")" -eq 0 ]; then
            [ -z "$dry_run" ] && kubectl delete ns $release_namespace
        fi
    fi
done < <(helm ls --all-namespaces)
[ $counter_delete -gt 0 ] || echo "No stale Helm charts found."
