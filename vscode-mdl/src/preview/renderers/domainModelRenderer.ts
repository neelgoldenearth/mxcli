// SPDX-License-Identifier: Apache-2.0

/**
 * Returns inline JavaScript code for the domain model renderer.
 * Depends on shared state: data, entityMap, categoryColors, collapsedEntities,
 * vscodeApi, and sketch helper functions.
 */
export function domainModelRendererJs(): string {
	return `
		// --- Domain Model renderer ---
		function renderDomainModel(layout) {
			var maxX = 0, maxY = 0;
			layout.children.forEach(function(node) {
				var right = node.x + node.width;
				var bottom = node.y + node.height;
				if (right > maxX) maxX = right;
				if (bottom > maxY) maxY = bottom;
			});

			var padding = 60;
			var titleHeight = 40;
			var headerHeight = 28;
			var attrLineHeight = 18;
			var svgWidth = maxX + padding * 2;
			var svgHeight = maxY + padding * 2 + titleHeight + 20;
			var fontFamily = diagramTheme === 'clean'
				? "var(--vscode-font-family, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif)"
				: "'Architects Daughter', cursive";

			var svg = '<svg xmlns="http://www.w3.org/2000/svg" width="' + svgWidth + '" height="' + svgHeight + '">';
			svg += '<defs>' + svgFilterDefs() + '</defs>';

			// Title
			var titleText = data.focusEntity
				? data.moduleName + '.' + data.focusEntity + ' \\u2014 Entity'
				: (data.moduleName ? data.moduleName + ' \\u2014 Domain Model' : 'Domain Model');
			svg += '<text x="' + padding + '" y="32" font-size="20" fill="' + inkColor + '" font-family="' + fontFamily + '">' + escHtml(titleText) + '</text>';

			// PoC badge (sketch only)
			if (diagramTheme === 'sketch') {
				var badgeRng = makeRng(42);
				var titleWidth = titleText.length * 11 + 10;
				var badgeX = padding + titleWidth;
				svg += '<path d="' + roughRoundedRect(badgeX, 17, 64, 20, 10, badgeRng) + '" fill="none" stroke="' + secondaryColor + '" stroke-width="1.2" filter="url(#pencil)"/>';
				svg += '<text x="' + (badgeX + 32) + '" y="31" font-size="9" fill="' + secondaryColor + '" text-anchor="middle" font-family="' + fontFamily + '">PoC draft</text>';
			}

			var offsetY = titleHeight;

			// Build edge lookup for association and generalization data
			var assocs = data.associations || [];
			var gens = data.generalizations || [];
			var assocEdgeIds = {};
			assocs.forEach(function(a) { assocEdgeIds[a.id] = a; });
			var genEdgeIds = {};
			gens.forEach(function(g, i) { genEdgeIds['gen-' + i] = g; });

			// Render edges (associations and generalizations)
			if (layout.edges) {
				layout.edges.forEach(function(edge) {
					var edgeRng = makeRng(hashStr(edge.id) + 7);
					var isGen = !!genEdgeIds[edge.id];
					var assocData = assocEdgeIds[edge.id];

					if (edge.sections) {
						edge.sections.forEach(function(section) {
							var pts = [{ x: section.startPoint.x + padding, y: section.startPoint.y + padding + offsetY }];
							if (section.bendPoints) {
								section.bendPoints.forEach(function(bp) {
									pts.push({ x: bp.x + padding, y: bp.y + padding + offsetY });
								});
							}
							pts.push({ x: section.endPoint.x + padding, y: section.endPoint.y + padding + offsetY });

							var d = '';
							for (var i = 0; i < pts.length - 1; i++) {
								var seg = roughLine(pts[i].x, pts[i].y, pts[i + 1].x, pts[i + 1].y, edgeRng, 1.0);
								if (i === 0) { d = seg; } else { d += seg.replace(/^M [^ ]+ [^ ]+/, ''); }
							}

							if (isGen) {
								// Dashed line for generalization
								svg += '<path d="' + d + '" fill="none" stroke="' + connectorColor + '" stroke-width="1.5" stroke-dasharray="6 4" opacity="0.6" stroke-linecap="round" filter="url(#pencil)"/>';
								// Triangle arrowhead at parent (target) end
								var last = pts[pts.length - 1];
								var prev = pts[pts.length - 2];
								var angle = Math.atan2(last.y - prev.y, last.x - prev.x);
								svg += roughTriangleArrow(last.x, last.y, angle, edgeRng);
							} else {
								// Solid line for association
								svg += '<path d="' + d + '" fill="none" stroke="' + connectorColor + '" stroke-width="1.5" opacity="0.6" stroke-linecap="round" filter="url(#pencil)"/>';

								// Cardinality markers
								var first = pts[0];
								var second = pts[1];
								var last = pts[pts.length - 1];
								var prev = pts[pts.length - 2];

								if (assocData) {
									// Source end (child/referenced entity): "1" for reference, "*" for referenceSet
									var srcAngle = Math.atan2(second.y - first.y, second.x - first.x);
									var srcPerp = srcAngle + Math.PI / 2;
									var srcLabel = assocData.type === 'referenceSet' ? '*' : '1';
									var srcLx = first.x + 14 * Math.cos(srcAngle) + 10 * Math.cos(srcPerp);
									var srcLy = first.y + 14 * Math.sin(srcAngle) + 10 * Math.sin(srcPerp);
									svg += '<text x="' + srcLx.toFixed(1) + '" y="' + srcLy.toFixed(1) + '" font-size="10" fill="' + secondaryColor + '" text-anchor="middle" font-family="' + fontFamily + '">' + srcLabel + '</text>';

									// Target end (parent/owner entity): "*" always
									var tgtAngle = Math.atan2(prev.y - last.y, prev.x - last.x);
									var tgtPerp = tgtAngle + Math.PI / 2;
									var tgtLx = last.x + 14 * Math.cos(tgtAngle) + 10 * Math.cos(tgtPerp);
									var tgtLy = last.y + 14 * Math.sin(tgtAngle) + 10 * Math.sin(tgtPerp);
									svg += '<text x="' + tgtLx.toFixed(1) + '" y="' + tgtLy.toFixed(1) + '" font-size="10" fill="' + secondaryColor + '" text-anchor="middle" font-family="' + fontFamily + '">*</text>';
								}
							}

							// Edge label (association name) at midpoint
							if (assocData && assocData.name) {
								var allPts = [section.startPoint];
								if (section.bendPoints) allPts.push.apply(allPts, section.bendPoints);
								allPts.push(section.endPoint);
								var midIdx = Math.floor(allPts.length / 2);
								var lx = allPts[midIdx].x + padding;
								var ly = allPts[midIdx].y + padding + offsetY - 6;
								svg += '<text x="' + lx + '" y="' + ly + '" font-size="9" fill="' + secondaryColor + '" opacity="0.7" text-anchor="middle" font-style="italic" font-family="' + fontFamily + '">' + escHtml(assocData.name) + '</text>';
							}
						});
					}
				});
			}

			// Render entity nodes
			layout.children.forEach(function(node) {
				var ent = entityMap[node.id];
				var cat = ent ? ent.category : 'persistent';
				var colors = categoryColors[cat] || categoryColors.persistent;
				var x = node.x + padding;
				var y = node.y + padding + offsetY;
				var w = node.width;
				var h = node.height;
				var nodeRng = makeRng(hashStr(node.id));

				var isFocus = ent && ent.isFocus;
				// Use qualified name for click navigation; external/ghost entities already have "Module.Entity" names
				var qualName = ent
					? (ent.name.indexOf('.') >= 0 ? ent.name : data.moduleName + '.' + ent.name)
					: node.id;
				svg += '<g class="entity-node" data-entity="' + escHtml(qualName) + '" data-node-id="' + escHtml(node.id) + '" style="cursor:pointer">';

				// Drop shadow for clean theme
				if (diagramTheme === 'clean') {
					svg += '<rect x="' + x + '" y="' + y + '" width="' + w + '" height="' + h + '" rx="4" fill="var(--vscode-editor-background, #1e1e1e)" filter="url(#clean-shadow)"/>';
				}

				// Focus entity: subtle glow outline behind the whole node
				if (isFocus) {
					var glowRng = makeRng(hashStr(node.id) + 999);
					svg += '<path d="' + roughRoundedRect(x - 3, y - 3, w + 6, h + 6, 6, glowRng) + '" fill="none" stroke="' + colors.base + '" stroke-width="3" opacity="0.3" stroke-linecap="round" filter="url(#pencil)"/>';
				}

				// Header section: marker fill in category color
				svg += markerFill(x, y, w, headerHeight, colors.light, makeRng(hashStr(node.id) + 50));

				// Header border (thicker for focus entity)
				var headerRng = makeRng(hashStr(node.id) + 100);
				var headerStroke = isFocus ? '2.5' : '1.5';
				svg += '<path d="' + roughRoundedRect(x, y, w, headerHeight, 4, headerRng) + '" fill="none" stroke="' + colors.base + '" stroke-width="' + headerStroke + '" stroke-linecap="round" filter="url(#pencil)"/>';

				// Entity name (centered in header)
				var entityName = ent ? ent.name : node.id;
				var hasAttrs = ent && ent.attributes && ent.attributes.length > 0;
				var isCollapsed = hasAttrs && !!collapsedEntities[node.id];

				svg += '<text x="' + (x + w / 2) + '" y="' + (y + 18) + '" font-size="13" fill="' + inkColor + '" font-weight="600" text-anchor="middle" font-family="' + fontFamily + '">' + escHtml(entityName) + '</text>';

				// Collapse/expand toggle icon on header
				if (hasAttrs) {
					var toggleIcon = isCollapsed ? '\\u25B6' : '\\u25BC';
					svg += '<text class="collapse-toggle" data-entity-id="' + escHtml(node.id) + '" x="' + (x + w - 16) + '" y="' + (y + 18) + '" font-size="10" fill="' + secondaryColor + '" text-anchor="middle" font-family="' + fontFamily + '" style="cursor:pointer">' + toggleIcon + '</text>';
				}

				// Attribute section
				if (hasAttrs && !isCollapsed) {
					var attrY = y + headerHeight;
					var attrH = h - headerHeight;

					// Light background for attribute section
					var attrRng = makeRng(hashStr(node.id) + 200);
					svg += '<path d="' + roughRoundedRect(x, attrY, w, attrH, 4, attrRng) + '" fill="none" stroke="' + colors.base + '" stroke-width="1" opacity="0.5" stroke-linecap="round" filter="url(#pencil)"/>';

					// Attribute rows
					ent.attributes.forEach(function(attr, i) {
						var rowY = attrY + 14 + i * attrLineHeight;
						svg += '<text x="' + (x + 8) + '" y="' + rowY + '" font-size="10" fill="' + secondaryColor + '" font-family="' + fontFamily + '">';
						svg += '<tspan fill="' + colors.base + '">' + escHtml(attr.type) + '</tspan>';
						svg += ' ' + escHtml(attr.name);
						svg += '</text>';
					});
				} else if (hasAttrs && isCollapsed) {
					// Collapsed: show attribute count
					var collY = y + headerHeight;
					var collH = h - headerHeight;
					if (collH > 2) {
						var collRng = makeRng(hashStr(node.id) + 200);
						svg += '<path d="' + roughRoundedRect(x, collY, w, collH, 4, collRng) + '" fill="none" stroke="' + colors.base + '" stroke-width="1" opacity="0.3" stroke-linecap="round" filter="url(#pencil)"/>';
						svg += '<text x="' + (x + w / 2) + '" y="' + (collY + 12) + '" font-size="9" fill="' + secondaryColor + '" opacity="0.6" text-anchor="middle" font-family="' + fontFamily + '">' + ent.attributes.length + ' attr' + (ent.attributes.length !== 1 ? 's' : '') + '</text>';
					}
				} else if (!ent || !ent.attributes || ent.attributes.length === 0) {
					// Empty body for entities with no attributes
					var emptyRng = makeRng(hashStr(node.id) + 300);
					var emptyY = y + headerHeight;
					var emptyH = h - headerHeight;
					if (emptyH > 2) {
						svg += '<path d="' + roughRoundedRect(x, emptyY, w, emptyH, 4, emptyRng) + '" fill="none" stroke="' + colors.base + '" stroke-width="1" opacity="0.3" stroke-linecap="round" filter="url(#pencil)"/>';
					}
				}

				svg += '</g>';
			});

			// Footer (sketch only)
			if (diagramTheme === 'sketch') {
				svg += '<text x="10" y="' + (svgHeight - 8) + '" font-size="10" fill="' + secondaryColor + '" opacity="0.4" font-family="' + fontFamily + '">sketch — subject to change</text>';
			}
			svg += '</svg>';

			var canvas = document.getElementById('diagram-canvas');
			canvas.innerHTML = svg;

			// Click handlers for collapse toggles
			canvas.querySelectorAll('.collapse-toggle').forEach(function(el) {
				el.addEventListener('click', function(e) {
					e.stopPropagation();
					var eid = el.getAttribute('data-entity-id');
					if (collapsedEntities[eid]) {
						delete collapsedEntities[eid];
					} else {
						collapsedEntities[eid] = true;
					}
					window.relayoutDomainModel();
				});
			});

			// Click handlers for entity nodes
			canvas.querySelectorAll('.entity-node').forEach(function(g) {
				g.addEventListener('click', function(e) {
					// Don't navigate if clicking the collapse toggle
					if (e.target.classList && e.target.classList.contains('collapse-toggle')) return;
					e.stopPropagation();
					var entityName = g.getAttribute('data-entity');
					var nodeId = g.getAttribute('data-node-id') || entityName;
					vscodeApi.postMessage({ type: 'nodeClicked', nodeId: nodeId });
					if (entityName) {
						vscodeApi.postMessage({ type: 'openEntity', entityName: entityName });
					}
				});
			});
		}
	`;
}
