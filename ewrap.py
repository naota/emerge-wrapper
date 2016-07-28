#!/usr/bin/python3.4

import os
import sys
import pygraphviz as pgz
import portage
from portage._sets.base import InternalPackageSet
from _emerge.main import parse_opts
from _emerge.create_depgraph_params import create_depgraph_params
from _emerge.depgraph import _backtrack_depgraph
from _emerge.resolver.slot_collision import slot_conflict_handler
portage._disable_legacy_globals()
portage.proxy.lazyimport.lazyimport(
    globals(),
    '_emerge.actions:load_emerge_config,run_action,action_build',
    '_emerge.stdout_spinner:stdout_spinner',
)


def fix_conflict(config, depgraph):
    depgraph._slot_conflict_handler = slot_conflict_handler(depgraph)
    handler = depgraph._slot_conflict_handler
    for root, slot_atom, pkgs in handler.all_conflicts:
        print("%s" % (slot_atom,))
        for p in pkgs:
            parents = handler.all_parents.get(p)
            if not parents:
                continue
            collisions = {}
            for ppkg, atom in parents:
                if atom.soname:
                    continue
                for other_pkg in pkgs:
                    atom_without_use_set = \
                                           InternalPackageSet(initial_atoms=(atom.without_use,))
                    if atom_without_use_set.findAtomForPackage(other_pkg,
                                                               modified_use=depgraph._pkg_use_enabled(other_pkg)):
                        continue
                    key = (atom.slot, atom.sub_slot, atom.slot_operator)
                    atoms = collisions.get(key, set())
                    atoms.add((ppkg, atom, other_pkg))
                    collisions[key] = atoms
                    print("collisions: %s" % collisions)
                    print("")
    return (config, False)


def draw_graph(digraph):
    def name(p):
        return "%r" % p
    G = pgz.AGraph(directed=True)
    for x in digraph:
        for y in digraph.parent_nodes(x):
            G.add_edge(name(y), name(x))
    G.layout('dot')
    G.draw("ewrap.png")

def profile_check(trees, myaction):
	if myaction in ("help", "info", "search", "sync", "version"):
		return os.EX_OK
	for root_trees in trees.values():
		if root_trees["root_config"].settings.profiles:
			continue
		# generate some profile related warning messages
		validate_ebuild_environment(trees)
		msg = ("Your current profile is invalid. If you have just changed "
			"your profile configuration, you should revert back to the "
			"previous configuration. Allowed actions are limited to "
			"--help, --info, --search, --sync, and --version.")
		writemsg_level("".join("!!! %s\n" % l for l in textwrap.wrap(msg, 70)),
			level=logging.ERROR, noiselevel=-1)
		return 1
	return os.EX_OK

def main():
    args = sys.argv[1:]
    args.extend(["--tree", "--ask"])
    myaction, myopts, myfiles = parse_opts(args, silent=True)

    os.umask(0o22)
    emerge_config = load_emerge_config(action=myaction, args=myfiles,
                                       opts=myopts)
    success, depgraph, favorites = False, None, None
    while not success:
        print("Building %s with %s" % (myfiles, myopts))
        success, depgraph, favorites = build(emerge_config)
        if not success:
            fixed = False
            emerge_config, fixed = fix_conflict(emerge_config, depgraph)
            if not fixed:
                break
    if success:
        # dynamic_config = depgraph._dynamic_config
        # draw_graph(dynamic_config.digraph.copy())
        # depgraph.display(depgraph.altlist(), favorites=favorites)
        emerge_config = load_emerge_config(emerge_config=emerge_config)
        run_action(emerge_config)
    else:
        depgraph.display_problems()


def build(emerge_config):
    settings = emerge_config.target_config.settings
    trees = emerge_config.trees
    myopts = emerge_config.opts
    myaction = emerge_config.action
    myparams = create_depgraph_params(myopts, myaction)
    myfiles = emerge_config.args
    spinner = stdout_spinner()
    spinner.update = spinner.update_quiet

    try:
        success, depgraph, favorites = _backtrack_depgraph(
            settings, trees, myopts, myparams, myaction, myfiles,
            spinner)
        return (success, depgraph, favorites)
    finally:
        # Call destructors for our portdbapi instances.
        for x in emerge_config.trees.values():
            if "porttree" in x.lazy_items:
                    continue
            x["porttree"].dbapi.close_caches()

main()
