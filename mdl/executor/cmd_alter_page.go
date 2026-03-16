// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/model"
	"github.com/mendixlabs/mxcli/sdk/mpr"
)

// execAlterPage handles ALTER PAGE/SNIPPET Module.Name { operations }.
func (e *Executor) execAlterPage(s *ast.AlterPageStmt) error {
	if e.reader == nil {
		return fmt.Errorf("not connected to a project")
	}
	if e.writer == nil {
		return fmt.Errorf("project not opened for writing")
	}

	h, err := e.getHierarchy()
	if err != nil {
		return fmt.Errorf("failed to build hierarchy: %w", err)
	}

	var unitID model.ID
	var containerID model.ID
	containerType := s.ContainerType
	if containerType == "" {
		containerType = "PAGE"
	}

	if containerType == "SNIPPET" {
		snippet, modID, err := e.findSnippetByName(s.PageName, h)
		if err != nil {
			return err
		}
		unitID = snippet.ID
		containerID = modID
	} else {
		page, err := e.findPageByName(s.PageName, h)
		if err != nil {
			return err
		}
		unitID = page.ID
		containerID = h.FindModuleID(page.ContainerID)
	}

	// Load raw BSON
	rawData, err := e.reader.GetRawUnit(unitID)
	if err != nil {
		return fmt.Errorf("failed to load raw %s data: %w", strings.ToLower(containerType), err)
	}

	// Resolve module name for building new widgets
	modName := h.GetModuleName(containerID)

	// Apply operations sequentially using the appropriate BSON finder
	findWidget := findBsonWidget // page default
	if containerType == "SNIPPET" {
		findWidget = findBsonWidgetInSnippet
	}

	for _, op := range s.Operations {
		switch o := op.(type) {
		case *ast.SetPropertyOp:
			if err := applySetPropertyWith(rawData, o, findWidget); err != nil {
				return fmt.Errorf("SET failed: %w", err)
			}
		case *ast.InsertWidgetOp:
			if err := e.applyInsertWidgetWith(rawData, o, modName, containerID, findWidget); err != nil {
				return fmt.Errorf("INSERT failed: %w", err)
			}
		case *ast.DropWidgetOp:
			if err := applyDropWidgetWith(rawData, o, findWidget); err != nil {
				return fmt.Errorf("DROP failed: %w", err)
			}
		case *ast.ReplaceWidgetOp:
			if err := e.applyReplaceWidgetWith(rawData, o, modName, containerID, findWidget); err != nil {
				return fmt.Errorf("REPLACE failed: %w", err)
			}
		default:
			return fmt.Errorf("unknown ALTER %s operation type: %T", containerType, op)
		}
	}

	// Marshal back to BSON bytes
	bytes, err := bson.Marshal(rawData)
	if err != nil {
		return fmt.Errorf("failed to marshal modified %s: %w", strings.ToLower(containerType), err)
	}

	// Save
	if err := e.writer.UpdateRawUnit(string(unitID), bytes); err != nil {
		return fmt.Errorf("failed to save modified %s: %w", strings.ToLower(containerType), err)
	}

	fmt.Fprintf(e.output, "Altered %s %s\n", strings.ToLower(containerType), s.PageName.String())
	return nil
}

// ============================================================================
// BSON widget tree walking
// ============================================================================

// bsonWidgetResult holds a found widget and its parent context.
type bsonWidgetResult struct {
	widget    map[string]any // the widget map itself
	parentArr []any          // the parent array containing the widget
	parentKey string         // key in the parent map that holds this array
	parentMap map[string]any // the map containing parentKey
	index     int            // index in parentArr
}

// widgetFinder is a function type for locating widgets in a raw BSON tree.
type widgetFinder func(rawData map[string]any, widgetName string) *bsonWidgetResult

// findBsonWidget searches the raw BSON page tree for a widget by name.
// Page format: FormCall.Arguments[].Widgets[]
func findBsonWidget(rawData map[string]any, widgetName string) *bsonWidgetResult {
	formCall, ok := rawData["FormCall"].(map[string]any)
	if !ok {
		return nil
	}

	args := getBsonArrayElements(formCall["Arguments"])
	for _, arg := range args {
		argMap, ok := arg.(map[string]any)
		if !ok {
			continue
		}
		if result := findInWidgetArray(argMap, "Widgets", widgetName); result != nil {
			return result
		}
	}
	return nil
}

// findBsonWidgetInSnippet searches the raw BSON snippet tree for a widget by name.
// Snippet format: Widgets[] (Studio Pro) or Widget.Widgets[] (mxcli).
func findBsonWidgetInSnippet(rawData map[string]any, widgetName string) *bsonWidgetResult {
	// Studio Pro format: top-level "Widgets" array
	if result := findInWidgetArray(rawData, "Widgets", widgetName); result != nil {
		return result
	}
	// mxcli format: "Widget" (singular) container with "Widgets" inside
	if widgetContainer, ok := rawData["Widget"].(map[string]any); ok {
		if result := findInWidgetArray(widgetContainer, "Widgets", widgetName); result != nil {
			return result
		}
	}
	return nil
}

// findInWidgetArray searches a widget array (by key in parentMap) for a named widget.
func findInWidgetArray(parentMap map[string]any, key string, widgetName string) *bsonWidgetResult {
	elements := getBsonArrayElements(parentMap[key])
	for i, elem := range elements {
		wMap, ok := elem.(map[string]any)
		if !ok {
			continue
		}
		name, _ := wMap["Name"].(string)
		if name == widgetName {
			return &bsonWidgetResult{
				widget:    wMap,
				parentArr: elements,
				parentKey: key,
				parentMap: parentMap,
				index:     i,
			}
		}
		// Recurse into children
		if result := findInWidgetChildren(wMap, widgetName); result != nil {
			return result
		}
	}
	return nil
}

// findInWidgetChildren recursively searches widget children for a named widget.
func findInWidgetChildren(wMap map[string]any, widgetName string) *bsonWidgetResult {
	typeName, _ := wMap["$Type"].(string)

	// Direct Widgets[] children (Container, DataView body, TabPage, GroupBox, etc.)
	if result := findInWidgetArray(wMap, "Widgets", widgetName); result != nil {
		return result
	}

	// FooterWidgets[] (DataView footer)
	if result := findInWidgetArray(wMap, "FooterWidgets", widgetName); result != nil {
		return result
	}

	// LayoutGrid: Rows[].Columns[].Widgets[]
	if strings.Contains(typeName, "LayoutGrid") {
		rows := getBsonArrayElements(wMap["Rows"])
		for _, row := range rows {
			rowMap, ok := row.(map[string]any)
			if !ok {
				continue
			}
			cols := getBsonArrayElements(rowMap["Columns"])
			for _, col := range cols {
				colMap, ok := col.(map[string]any)
				if !ok {
					continue
				}
				if result := findInWidgetArray(colMap, "Widgets", widgetName); result != nil {
					return result
				}
			}
		}
	}

	// TabContainer: TabPages[].Widgets[]
	if result := findInTabPages(wMap, widgetName); result != nil {
		return result
	}

	// ControlBar widgets
	if result := findInControlBar(wMap, widgetName); result != nil {
		return result
	}

	// CustomWidget (pluggable): Object.Properties[].Value.Widgets[]
	if strings.Contains(typeName, "CustomWidget") {
		if obj, ok := wMap["Object"].(map[string]any); ok {
			props := getBsonArrayElements(obj["Properties"])
			for _, prop := range props {
				propMap, ok := prop.(map[string]any)
				if !ok {
					continue
				}
				if valMap, ok := propMap["Value"].(map[string]any); ok {
					if result := findInWidgetArray(valMap, "Widgets", widgetName); result != nil {
						return result
					}
				}
			}
		}
	}

	return nil
}

// findInTabPages searches TabPages[].Widgets[] for a named widget.
func findInTabPages(wMap map[string]any, widgetName string) *bsonWidgetResult {
	tabPages := getBsonArrayElements(wMap["TabPages"])
	for _, tp := range tabPages {
		tpMap, ok := tp.(map[string]any)
		if !ok {
			continue
		}
		if result := findInWidgetArray(tpMap, "Widgets", widgetName); result != nil {
			return result
		}
	}
	return nil
}

// findInControlBar searches ControlBarItems within a ControlBar for a named widget.
func findInControlBar(wMap map[string]any, widgetName string) *bsonWidgetResult {
	controlBar, ok := wMap["ControlBar"].(map[string]any)
	if !ok {
		return nil
	}
	return findInWidgetArray(controlBar, "Items", widgetName)
}

// setBsonArray sets a BSON array with the type marker preserved.
// BSON arrays have format [int32(marker), item1, item2, ...].
func setBsonArray(parentMap map[string]any, key string, elements []any) {
	existing := toBsonArray(parentMap[key])
	var marker any
	if len(existing) > 0 {
		if _, ok := existing[0].(int32); ok {
			marker = existing[0]
		} else if _, ok := existing[0].(int); ok {
			marker = existing[0]
		}
	}
	if marker != nil {
		result := make([]any, 0, len(elements)+1)
		result = append(result, marker)
		result = append(result, elements...)
		parentMap[key] = result
	} else {
		parentMap[key] = elements
	}
}

// ============================================================================
// SET property
// ============================================================================

// applySetProperty modifies widget properties in the raw BSON tree (page format).
func applySetProperty(rawData map[string]any, op *ast.SetPropertyOp) error {
	return applySetPropertyWith(rawData, op, findBsonWidget)
}

// applySetPropertyWith modifies widget properties using the given widget finder.
func applySetPropertyWith(rawData map[string]any, op *ast.SetPropertyOp, find widgetFinder) error {
	if op.WidgetName == "" {
		// Page/snippet-level SET
		return applyPageLevelSet(rawData, op.Properties)
	}

	// Find the widget
	result := find(rawData, op.WidgetName)
	if result == nil {
		return fmt.Errorf("widget %q not found", op.WidgetName)
	}

	// Apply each property
	for propName, value := range op.Properties {
		if err := setRawWidgetProperty(result.widget, propName, value); err != nil {
			return fmt.Errorf("failed to set %s on %s: %w", propName, op.WidgetName, err)
		}
	}
	return nil
}

// applyPageLevelSet handles page-level SET (e.g., SET Title = 'New Title').
func applyPageLevelSet(rawData map[string]any, properties map[string]interface{}) error {
	for propName, value := range properties {
		switch propName {
		case "Title":
			// Title is stored as FormCall.Title or at the top level
			if formCall, ok := rawData["FormCall"].(map[string]any); ok {
				setTranslatableText(formCall, "Title", value)
			} else {
				setTranslatableText(rawData, "Title", value)
			}
		default:
			return fmt.Errorf("unsupported page-level property: %s", propName)
		}
	}
	return nil
}

// setRawWidgetProperty sets a property on a raw BSON widget map.
func setRawWidgetProperty(widget map[string]any, propName string, value interface{}) error {
	// Handle known standard BSON properties
	switch propName {
	case "Caption":
		return setWidgetCaption(widget, value)
	case "Content":
		return setWidgetContent(widget, value)
	case "Label":
		return setWidgetLabel(widget, value)
	case "ButtonStyle":
		if s, ok := value.(string); ok {
			widget["ButtonStyle"] = s
		}
		return nil
	case "Class":
		if appearance, ok := widget["Appearance"].(map[string]any); ok {
			if s, ok := value.(string); ok {
				appearance["Class"] = s
			}
		}
		return nil
	case "Style":
		if appearance, ok := widget["Appearance"].(map[string]any); ok {
			if s, ok := value.(string); ok {
				appearance["Style"] = s
			}
		}
		return nil
	case "Editable":
		if s, ok := value.(string); ok {
			widget["Editable"] = s
		}
		return nil
	case "Visible":
		if s, ok := value.(string); ok {
			widget["Visible"] = s
		} else if b, ok := value.(bool); ok {
			if b {
				widget["Visible"] = "True"
			} else {
				widget["Visible"] = "False"
			}
		}
		return nil
	case "Name":
		if s, ok := value.(string); ok {
			widget["Name"] = s
		}
		return nil
	default:
		// Try as pluggable widget property (quoted string property name)
		return setPluggableWidgetProperty(widget, propName, value)
	}
}

// setWidgetCaption sets the Caption property on a button or text widget.
func setWidgetCaption(widget map[string]any, value interface{}) error {
	caption, ok := widget["Caption"].(map[string]any)
	if !ok {
		// Try direct caption text
		setTranslatableText(widget, "Caption", value)
		return nil
	}
	setTranslatableText(caption, "", value)
	return nil
}

// setWidgetLabel sets the Label.Caption text on input widgets.
func setWidgetLabel(widget map[string]any, value interface{}) error {
	label, ok := widget["Label"].(map[string]any)
	if !ok {
		return nil
	}
	setTranslatableText(label, "Caption", value)
	return nil
}

// setWidgetContent sets the Content property on a DYNAMICTEXT widget.
// Content is stored as Forms$ClientTemplate → Template (Forms$Text) → Items[] → Translation{Text}.
// This mirrors extractTextContent which reads Content.Template.Items[].Text.
func setWidgetContent(widget map[string]any, value interface{}) error {
	strVal, ok := value.(string)
	if !ok {
		return fmt.Errorf("Content value must be a string")
	}
	content, ok := widget["Content"].(map[string]any)
	if !ok {
		return fmt.Errorf("widget has no Content property")
	}
	template, ok := content["Template"].(map[string]any)
	if !ok {
		return fmt.Errorf("Content has no Template")
	}
	items := getBsonArrayElements(template["Items"])
	if len(items) > 0 {
		if itemMap, ok := items[0].(map[string]any); ok {
			itemMap["Text"] = strVal
			return nil
		}
	}
	return fmt.Errorf("Content.Template has no Items with Text")
}

// setTranslatableText sets a translatable text value in BSON.
// If key is empty, modifies the map directly; otherwise navigates to map[key].
func setTranslatableText(parent map[string]any, key string, value interface{}) {
	strVal, ok := value.(string)
	if !ok {
		return
	}

	target := parent
	if key != "" {
		if nested, ok := parent[key].(map[string]any); ok {
			target = nested
		} else {
			// Try to set directly
			parent[key] = strVal
			return
		}
	}

	// Navigate to Translations[].Text
	translations := getBsonArrayElements(target["Translations"])
	if len(translations) > 0 {
		if tMap, ok := translations[0].(map[string]any); ok {
			tMap["Text"] = strVal
			return
		}
	}

	// Direct text value
	target["Text"] = strVal
}

// setPluggableWidgetProperty sets a property on a pluggable widget's Object.Properties[].
// Properties are identified by TypePointer referencing a PropertyType entry in the widget's
// Type.ObjectType.PropertyTypes array, NOT by a "Key" field on the property itself.
func setPluggableWidgetProperty(widget map[string]any, propName string, value interface{}) error {
	obj, ok := widget["Object"].(map[string]any)
	if !ok {
		return fmt.Errorf("property %q not found (widget has no pluggable Object)", propName)
	}

	// Build TypePointer ID -> PropertyKey map from Type.ObjectType.PropertyTypes
	propTypeKeyMap := make(map[string]string)
	if widgetType, ok := widget["Type"].(map[string]any); ok {
		var propTypes []any
		if objType, ok := widgetType["ObjectType"].(map[string]any); ok {
			propTypes = getBsonArrayElements(objType["PropertyTypes"])
		}
		for _, pt := range propTypes {
			ptMap, ok := pt.(map[string]any)
			if !ok {
				continue
			}
			key := extractString(ptMap["PropertyKey"])
			if key == "" {
				continue
			}
			id := extractBinaryID(ptMap["$ID"])
			if id != "" {
				propTypeKeyMap[id] = key
			}
		}
	}

	props := getBsonArrayElements(obj["Properties"])
	for _, prop := range props {
		propMap, ok := prop.(map[string]any)
		if !ok {
			continue
		}
		// Resolve property key via TypePointer
		typePointerID := extractBinaryID(propMap["TypePointer"])
		propKey := propTypeKeyMap[typePointerID]
		if propKey != propName {
			continue
		}
		// Set the value
		if valMap, ok := propMap["Value"].(map[string]any); ok {
			switch v := value.(type) {
			case string:
				valMap["PrimitiveValue"] = v
			case bool:
				if v {
					valMap["PrimitiveValue"] = "yes"
				} else {
					valMap["PrimitiveValue"] = "no"
				}
			case int:
				valMap["PrimitiveValue"] = fmt.Sprintf("%d", v)
			case float64:
				valMap["PrimitiveValue"] = fmt.Sprintf("%g", v)
			default:
				valMap["PrimitiveValue"] = fmt.Sprintf("%v", v)
			}
			return nil
		}
		return fmt.Errorf("property %q has no Value map", propName)
	}
	return fmt.Errorf("pluggable property %q not found in widget Object", propName)
}

// ============================================================================
// INSERT widget
// ============================================================================

// applyInsertWidget inserts new widgets before or after a target widget (page format).
func (e *Executor) applyInsertWidget(rawData map[string]any, op *ast.InsertWidgetOp, moduleName string, moduleID model.ID) error {
	return e.applyInsertWidgetWith(rawData, op, moduleName, moduleID, findBsonWidget)
}

// applyInsertWidgetWith inserts new widgets using the given widget finder.
func (e *Executor) applyInsertWidgetWith(rawData map[string]any, op *ast.InsertWidgetOp, moduleName string, moduleID model.ID, find widgetFinder) error {
	result := find(rawData, op.TargetName)
	if result == nil {
		return fmt.Errorf("widget %q not found", op.TargetName)
	}

	// Check for duplicate widget names before building
	for _, w := range op.Widgets {
		if w.Name != "" && find(rawData, w.Name) != nil {
			return fmt.Errorf("duplicate widget name '%s': a widget with this name already exists on the page", w.Name)
		}
	}

	// Find entity context from enclosing DataView/DataGrid/ListView
	entityCtx := findEnclosingEntityContext(rawData, op.TargetName)

	// Build new widget BSON from AST
	newBsonWidgets, err := e.buildWidgetsBson(op.Widgets, moduleName, moduleID, entityCtx)
	if err != nil {
		return fmt.Errorf("failed to build widgets: %w", err)
	}

	// Calculate insertion index
	insertIdx := result.index
	if op.Position == "AFTER" {
		insertIdx = result.index + 1
	}

	// Insert into the parent array
	newArr := make([]any, 0, len(result.parentArr)+len(newBsonWidgets))
	newArr = append(newArr, result.parentArr[:insertIdx]...)
	newArr = append(newArr, newBsonWidgets...)
	newArr = append(newArr, result.parentArr[insertIdx:]...)

	// Update parent
	setBsonArray(result.parentMap, result.parentKey, newArr)

	return nil
}

// ============================================================================
// DROP widget
// ============================================================================

// applyDropWidget removes widgets from the raw BSON tree (page format).
func applyDropWidget(rawData map[string]any, op *ast.DropWidgetOp) error {
	return applyDropWidgetWith(rawData, op, findBsonWidget)
}

// applyDropWidgetWith removes widgets using the given widget finder.
func applyDropWidgetWith(rawData map[string]any, op *ast.DropWidgetOp, find widgetFinder) error {
	for _, name := range op.WidgetNames {
		result := find(rawData, name)
		if result == nil {
			return fmt.Errorf("widget %q not found", name)
		}

		// Remove from parent array
		newArr := make([]any, 0, len(result.parentArr)-1)
		newArr = append(newArr, result.parentArr[:result.index]...)
		newArr = append(newArr, result.parentArr[result.index+1:]...)

		// Update parent
		setBsonArray(result.parentMap, result.parentKey, newArr)
	}
	return nil
}

// ============================================================================
// REPLACE widget
// ============================================================================

// applyReplaceWidget replaces a widget with new widgets (page format).
func (e *Executor) applyReplaceWidget(rawData map[string]any, op *ast.ReplaceWidgetOp, moduleName string, moduleID model.ID) error {
	return e.applyReplaceWidgetWith(rawData, op, moduleName, moduleID, findBsonWidget)
}

// applyReplaceWidgetWith replaces a widget using the given widget finder.
func (e *Executor) applyReplaceWidgetWith(rawData map[string]any, op *ast.ReplaceWidgetOp, moduleName string, moduleID model.ID, find widgetFinder) error {
	result := find(rawData, op.WidgetName)
	if result == nil {
		return fmt.Errorf("widget %q not found", op.WidgetName)
	}

	// Check for duplicate widget names (skip the widget being replaced)
	for _, w := range op.NewWidgets {
		if w.Name != "" && w.Name != op.WidgetName && find(rawData, w.Name) != nil {
			return fmt.Errorf("duplicate widget name '%s': a widget with this name already exists on the page", w.Name)
		}
	}

	// Find entity context from enclosing DataView/DataGrid/ListView
	entityCtx := findEnclosingEntityContext(rawData, op.WidgetName)

	// Build new widget BSON from AST
	newBsonWidgets, err := e.buildWidgetsBson(op.NewWidgets, moduleName, moduleID, entityCtx)
	if err != nil {
		return fmt.Errorf("failed to build replacement widgets: %w", err)
	}

	// Replace: remove old widget, insert new ones at same position
	newArr := make([]any, 0, len(result.parentArr)-1+len(newBsonWidgets))
	newArr = append(newArr, result.parentArr[:result.index]...)
	newArr = append(newArr, newBsonWidgets...)
	newArr = append(newArr, result.parentArr[result.index+1:]...)

	// Update parent
	setBsonArray(result.parentMap, result.parentKey, newArr)

	return nil
}

// ============================================================================
// Entity context extraction from BSON tree
// ============================================================================

// findEnclosingEntityContext walks the raw BSON tree to find the DataView, DataGrid,
// ListView, or Gallery ancestor of a target widget and extracts the entity name.
// This is needed for INSERT/REPLACE operations so that input widget Binds can be
// resolved to fully qualified attribute paths.
func findEnclosingEntityContext(rawData map[string]any, widgetName string) string {
	// Start from FormCall.Arguments[].Widgets[] (page format)
	if formCall, ok := rawData["FormCall"].(map[string]any); ok {
		args := getBsonArrayElements(formCall["Arguments"])
		for _, arg := range args {
			argMap, ok := arg.(map[string]any)
			if !ok {
				continue
			}
			if ctx := findEntityContextInWidgets(argMap, "Widgets", widgetName, ""); ctx != "" {
				return ctx
			}
		}
	}
	// Snippet format: Widgets[] or Widget.Widgets[]
	if ctx := findEntityContextInWidgets(rawData, "Widgets", widgetName, ""); ctx != "" {
		return ctx
	}
	if widgetContainer, ok := rawData["Widget"].(map[string]any); ok {
		if ctx := findEntityContextInWidgets(widgetContainer, "Widgets", widgetName, ""); ctx != "" {
			return ctx
		}
	}
	return ""
}

// findEntityContextInWidgets searches a widget array for the target widget,
// tracking entity context from DataView/DataGrid/ListView/Gallery ancestors.
func findEntityContextInWidgets(parentMap map[string]any, key string, widgetName string, currentEntity string) string {
	elements := getBsonArrayElements(parentMap[key])
	for _, elem := range elements {
		wMap, ok := elem.(map[string]any)
		if !ok {
			continue
		}
		name, _ := wMap["Name"].(string)
		if name == widgetName {
			return currentEntity
		}
		// Update entity context if this is a data container
		entityCtx := currentEntity
		if ent := extractEntityFromDataSource(wMap); ent != "" {
			entityCtx = ent
		}
		// Recurse into children
		if ctx := findEntityContextInChildren(wMap, widgetName, entityCtx); ctx != "" {
			return ctx
		}
	}
	return ""
}

// findEntityContextInChildren recursively searches widget children for the target,
// tracking entity context. Mirrors the traversal logic of findInWidgetChildren.
func findEntityContextInChildren(wMap map[string]any, widgetName string, currentEntity string) string {
	typeName, _ := wMap["$Type"].(string)

	// Direct Widgets[] children
	if ctx := findEntityContextInWidgets(wMap, "Widgets", widgetName, currentEntity); ctx != "" {
		return ctx
	}
	// FooterWidgets[]
	if ctx := findEntityContextInWidgets(wMap, "FooterWidgets", widgetName, currentEntity); ctx != "" {
		return ctx
	}
	// LayoutGrid: Rows[].Columns[].Widgets[]
	if strings.Contains(typeName, "LayoutGrid") {
		rows := getBsonArrayElements(wMap["Rows"])
		for _, row := range rows {
			rowMap, ok := row.(map[string]any)
			if !ok {
				continue
			}
			cols := getBsonArrayElements(rowMap["Columns"])
			for _, col := range cols {
				colMap, ok := col.(map[string]any)
				if !ok {
					continue
				}
				if ctx := findEntityContextInWidgets(colMap, "Widgets", widgetName, currentEntity); ctx != "" {
					return ctx
				}
			}
		}
	}
	// TabContainer: TabPages[].Widgets[]
	tabPages := getBsonArrayElements(wMap["TabPages"])
	for _, tp := range tabPages {
		tpMap, ok := tp.(map[string]any)
		if !ok {
			continue
		}
		if ctx := findEntityContextInWidgets(tpMap, "Widgets", widgetName, currentEntity); ctx != "" {
			return ctx
		}
	}
	// ControlBar
	if controlBar, ok := wMap["ControlBar"].(map[string]any); ok {
		if ctx := findEntityContextInWidgets(controlBar, "Items", widgetName, currentEntity); ctx != "" {
			return ctx
		}
	}
	// CustomWidget (pluggable): Object.Properties[].Value.Widgets[]
	if strings.Contains(typeName, "CustomWidget") {
		if obj, ok := wMap["Object"].(map[string]any); ok {
			props := getBsonArrayElements(obj["Properties"])
			for _, prop := range props {
				propMap, ok := prop.(map[string]any)
				if !ok {
					continue
				}
				if valMap, ok := propMap["Value"].(map[string]any); ok {
					if ctx := findEntityContextInWidgets(valMap, "Widgets", widgetName, currentEntity); ctx != "" {
						return ctx
					}
				}
			}
		}
	}
	return ""
}

// extractEntityFromDataSource extracts the entity qualified name from a widget's
// DataSource BSON. Handles DataView, DataGrid, ListView, and Gallery data sources.
func extractEntityFromDataSource(wMap map[string]any) string {
	ds, ok := wMap["DataSource"].(map[string]any)
	if !ok {
		return ""
	}
	// EntityRef.Entity contains the qualified name (e.g., "Module.Entity")
	if entityRef, ok := ds["EntityRef"].(map[string]any); ok {
		if entity, ok := entityRef["Entity"].(string); ok {
			return entity
		}
	}
	return ""
}

// ============================================================================
// Widget BSON building
// ============================================================================

// buildWidgetsBson converts AST widgets to raw BSON map[string]any values.
func (e *Executor) buildWidgetsBson(widgets []*ast.WidgetV3, moduleName string, moduleID model.ID, entityContext string) ([]any, error) {
	pb := &pageBuilder{
		writer:           e.writer,
		reader:           e.reader,
		moduleID:         moduleID,
		moduleName:       moduleName,
		entityContext:    entityContext,
		widgetScope:      make(map[string]model.ID),
		paramScope:       make(map[string]model.ID),
		paramEntityNames: make(map[string]string),
		execCache:        e.cache,
		fragments:        e.fragments,
	}

	var result []any
	for _, w := range widgets {
		bsonD, err := pb.buildWidgetV3ToBSON(w)
		if err != nil {
			return nil, fmt.Errorf("failed to build widget %s: %w", w.Name, err)
		}
		if bsonD == nil {
			continue
		}

		// Convert bson.D → map[string]any via marshal/unmarshal round-trip
		rawMap, err := bsonDToMap(bsonD)
		if err != nil {
			return nil, fmt.Errorf("failed to convert widget BSON: %w", err)
		}
		result = append(result, rawMap)
	}
	return result, nil
}

// bsonDToMap converts a bson.D to map[string]any via marshal/unmarshal.
func bsonDToMap(d bson.D) (map[string]any, error) {
	bytes, err := bson.Marshal(d)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := bson.Unmarshal(bytes, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ============================================================================
// Helper: SerializeWidget is already available via mpr package
// ============================================================================

var _ = mpr.SerializeWidget // ensure import is used
