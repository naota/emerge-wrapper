from _emerge.Package import Package
from _emerge.actions import load_emerge_config
from _emerge.create_depgraph_params import create_depgraph_params
from _emerge.depgraph import _backtrack_depgraph
from _emerge.main import parse_opts
from _emerge.resolver.slot_collision import slot_conflict_handler
from _emerge.stdout_spinner import stdout_spinner

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
    success, depgraph, favorites = False, None, None
    while not success:
        action, opts, files = parse_opts(args, silent=True)
        config = load_emerge_config(action=action, args=files, opts=opts)
        print(("Targets: %s\nOptions: %s\nCalculating dependency  "
               % (files, opts)), end="")
        success, depgraph, favorites = calcdep(config)
        print()
        if success:
            break
        newopts = []
        depgraph.display_problems()
        newopts += fix_conflict(config, depgraph)
        added = False
        for opt in newopts:
            if opt not in args:
                print("Adding %s" % opt)
                args.append(opt)
                added = True
        if not added:
            return False, depgraph
    return True, depgraph


def fix_conflict(config, depgraph):
    depgraph._slot_conflict_handler = slot_conflict_handler(depgraph)
    handler = depgraph._slot_conflict_handler
    newpkg = set()
    for _, slot_atom, pkgs in handler.all_conflicts:
        for p in pkgs:
            parents = handler.all_parents.get(p)
            if not parents:
                continue
            for ppkg, atom in parents:
                if not isinstance(ppkg, Package):
                    continue
                if ppkg.operation != "merge":
                    newpkg.add(ppkg)
    if newpkg:
        # return [p.cp for p in newpkg]
        return ["--reinstall-atoms=" + " ".join([p.cp for p in newpkg])]
    return []
