#!/usr/bin/python3.4

import os
import portage
from portage._sets.base import InternalPackageSet
from _emerge.create_depgraph_params import create_depgraph_params
from _emerge.depgraph import _backtrack_depgraph
from _emerge.resolver.slot_collision import slot_conflict_handler
portage._disable_legacy_globals()
portage.proxy.lazyimport.lazyimport(
        globals(),
        '_emerge.actions:load_emerge_config,run_action,action_build',
        '_emerge.stdout_spinner:stdout_spinner',
)

def main():
        myaction = ""
        myopts = {}
        askopts = {"--ask": True, "--verbose": True, "--pretend": True}
        worldopts = {"--update": True, "--deep": True, "--newuse": True, "--tree": True}

        myfiles = ["world"]
        myopts = worldopts

        # myfiles = ["portage"]
        # myopts = {}

        os.umask(0o22)
        emerge_config = load_emerge_config(
                action=myaction, args=myfiles, opts=myopts)
        success = False
        while not success:
                print("Building with %s" % myfiles)
                success, depgraph = build(emerge_config)
                if not success:
                        depgraph._slot_conflict_handler = slot_conflict_handler(depgraph)
                        handler = depgraph._slot_conflict_handler
                        for root, slot_atom, pkgs in handler.all_conflicts:
                                print("%s" % (slot_atom,))
                                for p in pkgs:
                                        target = p.cp
                                        parents = handler.all_parents.get(p)
                                        if not parents:
                                                continue
                                        collisions = {}
                                        for ppkg, atom in parents:
                                                if atom.soname:
                                                        continue
                                                for other_pkg in pkgs:
                                                        atom_without_use_set = InternalPackageSet(initial_atoms=(atom.without_use,))
                                                        if atom_without_use_set.findAtomForPackage(other_pkg, modified_use=depgraph._pkg_use_enabled(other_pkg)):
                                                                continue
                                                        key = (atom.slot, atom.sub_slot, atom.slot_operator)
                                                        atoms = collisions.get(key, set())
                                                        atoms.add((ppkg, atom, other_pkg))
                                                        collisions[key] = atoms
                                        print("collisions: %s" % collisions)
                                print("")
                        return

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
                success, mydepgraph, favorites = _backtrack_depgraph(
                        settings, trees, myopts, myparams, myaction, myfiles, spinner)
                return (success, mydepgraph)
                # print("graph")
                # for x in mydepgraph.altlist():
                #         print("%r" % x)
                # mydepgraph.display(
                #         mydepgraph.altlist(),
                #         favorites=favorites)
        finally:
                # Call destructors for our portdbapi instances.
                for x in emerge_config.trees.values():
                        if "porttree" in x.lazy_items:
                                continue
                        x["porttree"].dbapi.close_caches()

main()
