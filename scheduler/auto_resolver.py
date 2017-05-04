from _emerge.Package import Package
from _emerge.actions import load_emerge_config
from _emerge.create_depgraph_params import create_depgraph_params
from _emerge.depgraph import _backtrack_depgraph
from _emerge.main import parse_opts
from _emerge.resolver.slot_collision import slot_conflict_handler
from _emerge.stdout_spinner import stdout_spinner
from portage._sets.base import InternalPackageSet

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

def autoresolve(args):
    success, depgraph = False, None
    while not success:
        action, opts, files = parse_opts(args, silent=True)
        config = load_emerge_config(action=action, args=files, opts=opts)
        print(("Targets: %s\nOptions: %s\nCalculating dependency  "
               % (files, opts)), end="")
        # print("Calculating dependency  ", end="")
        success, depgraph, _ = calcdep(config)
        print()
        if success:
            break
        newpkgs = []
        depgraph.display_problems()
        newpkgs += fix_conflict(depgraph)
        added = False
        reinstalls = opts.get("--reinstall-atoms", [])
        oneshot = opts.get("--oneshot", False)
        for pkg in newpkgs:
            if pkg not in files and pkg not in reinstalls:
                # print("Adding %s" % opt)
                if oneshot:
                    args.append(pkg)
                else:
                    args.append("--reinstall-atoms="+pkg)
                added = True
        if not added:
            return False, depgraph, args
    return True, depgraph, args


def fix_conflict(depgraph):
    # non-supported problems
    have_slot_conflict = any(depgraph._dynamic_config._package_tracker.slot_conflicts())
    if not have_slot_conflict:
        print("skip: no slot conflict")
        return []
    dynamic_config = depgraph._dynamic_config
    if dynamic_config._missing_args:
        print("skip: missing args")
        return []
    if dynamic_config._pprovided_args:
        print("skip: pprovided args")
        return []
    if dynamic_config._masked_license_updates:
        print("skip: masked license")
        return []
    if dynamic_config._masked_installed:
        print("skip: masked installed")
        return []
    #if dynamic_config._needed_unstable_keywords:
    #    print("skip: needed unstable: %s" % dynamic_config._needed_unstable_keywords)
    #    return []
    if dynamic_config._needed_p_mask_changes:
        print("skip: needed package mask")
        return []
    if dynamic_config._needed_use_config_changes.items():
        print("skip: needed use config changes")
        return []
    if dynamic_config._needed_license_changes.items():
        print("skip: needed license change")
        return []
    if dynamic_config._unsatisfied_deps_for_display:
        res = []
        for pargs, kwargs in dynamic_config._unsatisfied_deps_for_display:
            print("%s %s" % (pargs, kwargs))
            if "myparent" in kwargs and kwargs["myparent"].operation == "nomerge":
                ppkg = kwargs["myparent"]
                res.append(ppkg.cp)
        return res

    _pkg_use_enabled = depgraph._pkg_use_enabled
    depgraph._slot_conflict_handler = slot_conflict_handler(depgraph)
    handler = depgraph._slot_conflict_handler
    newpkg = set()
    print(handler.all_conflicts)
    for _, _, pkgs in handler.all_conflicts:
        for pkg in pkgs[1:]:
            parents = handler.all_parents.get(pkg)
            if not parents:
                continue
            for ppkg, atom in parents:
                if not isinstance(ppkg, Package):
                    continue
                if atom.soname:
                    continue
                for other_pkg in pkgs:
                    if pkg == other_pkg:
                        continue
                    atom_without_use_set = InternalPackageSet(
                        initial_atoms=(atom.without_use,))
                    atom_without_use_and_slot_set = InternalPackageSet(
                        initial_atoms=(atom.without_use.without_slot,))
                    if atom_without_use_and_slot_set.findAtomForPackage(
                            other_pkg, modified_use=_pkg_use_enabled(other_pkg)) and \
                        atom_without_use_set.findAtomForPackage(
                            other_pkg, modified_use=_pkg_use_enabled(other_pkg)):
                        continue
                    if ppkg.operation != "merge":
                        print("reinstall %s for %s" % (ppkg, pkg))
                        newpkg.add(ppkg)
    if newpkg:
        return [p.cp for p in newpkg]
        # return ["--reinstall-atoms=" + " ".join([pkg.cp for pkg in newpkg])]
    return []
