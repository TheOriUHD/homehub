package main

// The projection.
//
// A Caddy config carries secrets: basic_auth password hashes, credentials set on
// headers going upstream, and ACME external-account-binding HMAC keys. None of
// them are needed to answer "which domain points at which upstream", so none of
// them are sent.
//
// This is an ALLOWLIST of keys, not a denylist of sensitive ones. A denylist
// silently leaks whatever Caddy adds in its next version; an allowlist silently
// drops it, and dropping a field devtracker needed is a visible bug rather than
// an invisible disclosure.
//
// The nested shape is preserved exactly, so devtracker's parser is identical
// whether it is handed one of these or a config pasted by hand. A test on that
// side runs this code over a real `caddy adapt` fixture and asserts both parse
// to the same result, because this allowlist and that parser are two halves of
// one contract written in two languages.

type object = map[string]any

func asObject(v any) (object, bool) {
	m, ok := v.(object)
	return m, ok
}

func asArray(v any) []any {
	a, ok := v.([]any)
	if !ok {
		return nil
	}
	return a
}

func asStrings(v any) []string {
	items := asArray(v)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func projectHandler(v any) object {
	handler, ok := asObject(v)
	if !ok {
		return nil
	}

	out := object{}

	if name, ok := handler["handler"].(string); ok {
		out["handler"] = name
	}

	// Nested routes: `subroute`, and anything else carrying routes.
	if routes, ok := handler["routes"]; ok {
		out["routes"] = projectRoutes(routes)
	}

	if raw, ok := handler["upstreams"]; ok {
		upstreams := []any{}
		for _, u := range asArray(raw) {
			// `dial` only. An upstream can also carry TLS client-certificate
			// settings, which are none of devtracker's business.
			if up, ok := asObject(u); ok {
				if dial, ok := up["dial"].(string); ok {
					upstreams = append(upstreams, object{"dial": dial})
				}
			}
		}
		if len(upstreams) > 0 {
			out["upstreams"] = upstreams
		}
	}

	// Presence is all that is read. The contents describe how upstreams are
	// discovered and are not needed to report "this cannot be resolved".
	if handler["dynamic_upstreams"] != nil {
		out["dynamic_upstreams"] = object{}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func projectRoutes(v any) []any {
	out := []any{}

	for _, item := range asArray(v) {
		route, ok := asObject(item)
		if !ok {
			continue
		}

		projected := object{}

		matchers := []any{}
		for _, m := range asArray(route["match"]) {
			matcher, ok := asObject(m)
			if !ok {
				continue
			}
			keep := object{}
			for _, key := range []string{"host", "path"} {
				if values := asStrings(matcher[key]); len(values) > 0 {
					keep[key] = values
				}
			}
			if len(keep) > 0 {
				matchers = append(matchers, keep)
			}
		}
		if len(matchers) > 0 {
			projected["match"] = matchers
		}

		handlers := []any{}
		for _, h := range asArray(route["handle"]) {
			if ph := projectHandler(h); ph != nil {
				handlers = append(handlers, ph)
			}
		}
		if len(handlers) > 0 {
			projected["handle"] = handlers
		}

		if len(projected) > 0 {
			out = append(out, projected)
		}
	}

	return out
}

// ProjectConfig reduces a Caddy config to the domain-to-upstream map, and
// nothing else.
func ProjectConfig(v any) object {
	config, ok := asObject(v)
	if !ok {
		return object{}
	}

	apps, ok := asObject(config["apps"])
	if !ok {
		return object{}
	}
	httpApp, ok := asObject(apps["http"])
	if !ok {
		return object{}
	}
	servers, ok := asObject(httpApp["servers"])
	if !ok {
		return object{}
	}

	projected := object{}
	for name, s := range servers {
		server, ok := asObject(s)
		if !ok {
			continue
		}
		entry := object{"routes": projectRoutes(server["routes"])}
		// Not read by the parser, but a port list is not a secret and it lets a
		// human confirm they are looking at the server they expected.
		if listen := asStrings(server["listen"]); len(listen) > 0 {
			entry["listen"] = listen
		}
		projected[name] = entry
	}

	return object{"apps": object{"http": object{"servers": projected}}}
}
