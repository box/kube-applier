#!/bin/bash



# TODO:
# Can we write these scripts in pyhton ?
# Do jenkins machines have python installed ?

set -e
set -x
# Need extended glob for negative file matching.
shopt -s extglob

get_dir_containing_script() {
    local source="${BASH_SOURCE[0]}"
    while [ -h "$source" ]; do # resolve $source until the file is no longer a symlink
        local dir="$( cd -P "$( dirname "$source" )" && pwd )"
        source="$(readlink "$source")"
         # if $SOURCE was a relative symlink, we need to resolve it relative to the path where the symlink file was located
        [[ $source != /* ]] && source="$dir/$source"
    done
    dir="$( cd -P "$( dirname "$source" )" && pwd )"
    echo $dir
}

# Install go at the given version at the given directory.  The desired version
# string is passed as the first parameter. The installation directory is passed
# as the second parameter. The installation directory is relative to the
# current directory.  Since in jenkins jobs, we do not have permission to
# modify the system directories, this function installs go binary to a
# directory that the user has permissions to and extends the PATH accodingly.
# Example usage:
# install_go "1.6.2" "go_install"
function install_go() {
   # Creating a subshell so that changes in this function do not "escape" the
   # function. For example change directory.

   local go_version=$1
   local install_dir=$2


   local cwd=$(pwd)
   local parent_dir=$(get_dir_containing_script)
   local run_dir="${parent_dir}/${install_dir}"

   mkdir -p "${run_dir}"
   cd "${run_dir}"

   local go_binary=go${go_version}.linux-amd64.tar.gz
   echo "Installing go ${go_version}..."
   wget -q  https://storage.googleapis.com/golang/$go_binary
   tar -xzf $go_binary

   # This is an unusual installation location for go. Need to update GOROOT
   # So that we can execute go later on.
   export GOROOT=${run_dir}/go
   # Extend the path accordingly to include the new extracted directory.
   export PATH="${GOROOT}/bin:${PATH}"
   echo "Installed go ${go_version}."

   cd ${cwd}
}

# Create a gopath directory and copy all the files in the repo into an
# appropriate directory in the gopath. Set the GOPATH and PATH environment
# variables. Return the directory name of the extracted directory.
function setup_repo_in_gopath() {

   local ignore_dir=$1

   local cwd=$(pwd)

   local parent_dir=$(get_dir_containing_script)
   local remote_url=$(git ls-remote --get-url origin)
   local basename=$(basename $remote_url)
   local repo_name=${basename%.*}
   local gopath_basename="gopath"
   local gopath="${parent_dir}/${gopath_basename}"

   cd "${parent_dir}"

   export GOPATH="${gopath}"
   export PATH=${GOPATH//://bin:}/bin:$PATH

   local gopath_src="${gopath}/src"
   local gopath_github="${gopath_src}/github.com"
   # TODO: We need to get the organization or user name programmatically.
   # It is actually your parent parent dir
   local gopath_org="${gopath_github}/box"
   local gopath_repo_root="${gopath_org}/${repo_name}"

   mkdir -p "${gopath_repo_root}"

   # Copy all repo files, except the recently created
   cp -r !("${gopath_basename}"|"${ignore_dir}") "${gopath_repo_root}"

   return_setup_repo_in_gopath="${gopath_repo_root}"
}


go_install_dir="go_install"
install_go "1.6.2" ${go_install_dir}

# TODO: Using a distorted method of getting the return value from the
# function without calling it in a subshell
setup_repo_in_gopath ${go_install_dir}
cd ${return_setup_repo_in_gopath}

go test -v ./...



