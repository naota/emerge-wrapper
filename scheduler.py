#!/usr/bin/python3

from datetime import datetime
import os
from os import path
import subprocess
import sys
import threading
from _emerge.Package import Package
from _emerge.actions import load_emerge_config
from _emerge.create_depgraph_params import create_depgraph_params
from _emerge.depgraph import _backtrack_depgraph
from _emerge.main import parse_opts
from _emerge.stdout_spinner import stdout_spinner


def main():
    args = sys.argv[1:]
    args.extend(["--tree", "--pretend", "--usepkg"])
    action, opts, files = parse_opts(args, silent=True)
    config = load_emerge_config(action=action, args=files, opts=opts)
    success, depgraph, favorites = False, None, None
    while not success:
        print(("Targets: %s\nOptions: %s\nCalculating dependency"
               % (files, opts)), end="")
        success, depgraph, favorites = calcdep(config)
        print()
        break
    if not success:
        print("Failed to calculate dependency!")
        depgraph.display_problems()
        return
    graph = depgraph._dynamic_config.digraph
    built = set()
    logdir = path.join("log", datetime.now().strftime("%Y%m%d-%H%M%S"))
    manager = Manager()
    os.makedirs(logdir)
    for root in graph.root_nodes():
        for _, child in graph.bfs(root):
            if not isinstance(child, Package):
                continue
            if child.operation != "merge" or child.type_name == "binary":
                continue
            if child not in built:
                proc = Builder(graph, child, built, logdir, manager)
                built |= proc.tobuild
                manager.start(proc)
    manager.loop()


def clear():
    # print("\x1b[2J\x1b[H", end="")
    print("\x1b[2J")


class Manager():

    def __init__(self):
        self.binary_lock = threading.Lock()
        self.finished_lock = threading.Lock()
        self.status_event = threading.Condition()
        self.finished_event = threading.Condition()
        self.finished = set()
        self.finished_fail = set()
        self.procs = []

    def start(self, proc):
        self.procs.append(proc)
        proc.start()

    def finish(self, pkgs, result):
        with self.finished_lock:
            if result == 0:
                self.finished |= pkgs
            else:
                for p in pkgs:
                    if os.access(binary_path(p), os.R_OK):
                        self.finished.add(p)
                    else:
                        self.finished_fail.add(p)
        with self.finished_event:
            self.finished_event.notify_all()

    def loop(self):
        maxlen = max([len(x.name) for x in self.procs])
        alive = True
        while alive:
            clear()
            # print("=========================")
            alive = False
            for p in self.procs:
                print(("{:<%d}\t{}" % maxlen).format(p.name, p.status))
                alive |= p.is_alive()
            self.wait_status()
        for p in self.procs:
            p.join()

    def request_packages(self, pkgs):
        with self.binary_lock:
            tobuild = []
            for p in pkgs:
                if not os.access(binary_path(p), os.R_OK):
                    tobuild.append("=%s" % p.cpv)
            if tobuild:
                return subprocess.call(["quickpkg"] + tobuild,
                                       stdout=subprocess.DEVNULL,
                                       stderr=subprocess.DEVNULL)
            return 0

    def wait_finished(self):
        with self.finished_event:
            self.finished_event.wait(60)

    def wait_status(self):
        with self.status_event:
            self.status_event.wait()


class Builder(threading.Thread):

    def __init__(self, graph, root, built, logdir, manager):
        super(Builder, self).__init__()
        self.name = root.cpv
        self.rootpkg = root
        self.logfile = path.join(logdir, "%s.log" % root.cpv.replace("/", "_"))
        self.manager = manager
        installed, waiting, tobuild = set(), set(), set()
        for _, pkg in graph.bfs(root):
            if pkg.operation == "nomerge" or pkg.type_name == "binary":
                installed.add(pkg)
            elif pkg in built:
                waiting.add(pkg)
            else:
                tobuild.add(pkg)
        self.installed = installed
        self.waiting = waiting
        self.tobuild = tobuild
        self.set_status("Initialized")

    def run(self):
        self.set_status("Requesting binary: %s" %
                        name_packages(list(self.installed)))
        if self.manager.request_packages(self.installed) != 0:
            self.finish(-1, "Dependency binary failed: %s" %
                        name_packages(list(self.installed)))
        failed = self.update_finished()
        while self.waiting:
            self.set_status("Waiting for %s -> %s" %
                            (name_packages(list(self.waiting)),
                             name_packages(list(self.tobuild))))
            self.manager.wait_finished()
            failed = self.update_finished()
        if failed:
            self.finish(-1, "Dependency failed: %s" %
                        name_packages(list(failed)))
            return
        atom = "=%s" % self.rootpkg.cpv
        self.set_status("Building %s" % name_packages(list(self.tobuild)))
        with open(self.logfile, "w+") as f:
            r = subprocess.call(["./build.sh", atom], stdout=f, stderr=f)
        if r == 0:
            self.finish(r, "Finished: %s" % name_packages(self.tobuild))
        else:
            self.finish(r, "Faild: %s" % name_packages(self.tobuild))

    def update_finished(self):
        with self.manager.finished_lock:
            failed = self.waiting & self.manager.finished_fail
            if failed:
                self.waiting = set()
                return failed
            self.waiting -= self.manager.finished
        for p in self.waiting:
            if os.access(binary_path(p), os.R_OK):
                self.waiting.remove(p)
        return set()

    def finish(self, result, msg):
        self.manager.finish(self.tobuild, result)
        self.set_status(msg)

    def set_status(self, stat):
        with self.manager.status_event:
            self.status = stat
            self.manager.status_event.notify()


def name_packages(pkgs, n=5):
    if len(pkgs) <= n:
        return ", ".join([p.cpv for p in pkgs])
    else:
        return "%s and %d more packages" \
            % (", ".join([p.cpv for p in pkgs[:n]]), len(pkgs) - n)


def binary_path(pkg):
    return "/usr/portage/packages/%s.tbz2" % pkg.cpv


def calcdep(config):
    settings = config.target_config.settings
    trees = config.trees
    opts = config.opts
    action = config.action
    params = create_depgraph_params(opts, action)
    files = config.args
    spinner = stdout_spinner()
    # spinner.update = spinner.update_quiet
    return _backtrack_depgraph(settings, trees, opts, params, action, files,
                               spinner)

main()
