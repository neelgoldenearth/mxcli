// SPDX-License-Identifier: Apache-2.0

// Package executor - Enumeration commands (SHOW/DESCRIBE/CREATE/ALTER/DROP ENUMERATION)
package executor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/model"
)

// execCreateEnumeration handles CREATE ENUMERATION statements.
func (e *Executor) execCreateEnumeration(s *ast.CreateEnumerationStmt) error {
	if e.reader == nil {
		return fmt.Errorf("not connected to a project")
	}

	// Validate enumeration values for reserved words
	if errs := ValidateEnumeration(s); len(errs) > 0 {
		return fmt.Errorf("invalid enumeration '%s':\n  - %s",
			s.Name.String(), strings.Join(errs, "\n  - "))
	}

	// Find module
	module, err := e.findModule(s.Name.Module)
	if err != nil {
		return err
	}

	// Check if enumeration already exists
	existingEnum := e.findEnumeration(s.Name.Module, s.Name.Name)
	if existingEnum != nil && !s.CreateOrModify {
		return fmt.Errorf("enumeration already exists: %s.%s (use CREATE OR MODIFY to update)", s.Name.Module, s.Name.Name)
	}

	// Create enumeration values
	var values []model.EnumerationValue
	for _, v := range s.Values {
		values = append(values, model.EnumerationValue{
			Name: v.Name,
			Caption: &model.Text{
				Translations: map[string]string{"en_US": v.Caption},
			},
		})
	}

	// If enumeration exists and CREATE OR MODIFY, delete it first
	if existingEnum != nil && s.CreateOrModify {
		if err := e.writer.DeleteEnumeration(existingEnum.ID); err != nil {
			return fmt.Errorf("failed to delete existing enumeration: %w", err)
		}
	}

	// Create enumeration
	enum := &model.Enumeration{
		ContainerID:   module.ID,
		Name:          s.Name.Name,
		Documentation: s.Documentation,
		Values:        values,
	}

	if err := e.writer.CreateEnumeration(enum); err != nil {
		return fmt.Errorf("failed to create enumeration: %w", err)
	}

	// Invalidate hierarchy cache so the new enumeration's container is visible
	e.invalidateHierarchy()

	fmt.Fprintf(e.output, "Created enumeration: %s\n", s.Name)
	return nil
}

// findEnumeration finds an enumeration by module and name.
func (e *Executor) findEnumeration(moduleName, enumName string) *model.Enumeration {
	enums, err := e.reader.ListEnumerations()
	if err != nil {
		return nil
	}

	h, err := e.getHierarchy()
	if err != nil {
		return nil
	}

	for _, enum := range enums {
		modID := h.FindModuleID(enum.ContainerID)
		modName := h.GetModuleName(modID)
		if enum.Name == enumName && modName == moduleName {
			return enum
		}
	}
	return nil
}

// execAlterEnumeration handles ALTER ENUMERATION statements.
func (e *Executor) execAlterEnumeration(s *ast.AlterEnumerationStmt) error {
	// TODO: Implement ALTER ENUMERATION
	return fmt.Errorf("ALTER ENUMERATION not yet implemented")
}

// execDropEnumeration handles DROP ENUMERATION statements.
func (e *Executor) execDropEnumeration(s *ast.DropEnumerationStmt) error {
	if e.reader == nil {
		return fmt.Errorf("not connected to a project")
	}

	// Find enumeration
	enums, err := e.reader.ListEnumerations()
	if err != nil {
		return fmt.Errorf("failed to list enumerations: %w", err)
	}

	for _, enum := range enums {
		if enum.Name == s.Name.Name {
			// Check module matches
			module, err := e.findModuleByID(enum.ContainerID)
			if err == nil && (s.Name.Module == "" || module.Name == s.Name.Module) {
				if err := e.writer.DeleteEnumeration(enum.ID); err != nil {
					return fmt.Errorf("failed to delete enumeration: %w", err)
				}
				fmt.Fprintf(e.output, "Dropped enumeration: %s\n", s.Name)
				return nil
			}
		}
	}

	return fmt.Errorf("enumeration not found: %s", s.Name)
}

// showEnumerations handles SHOW ENUMERATIONS command.
func (e *Executor) showEnumerations(moduleName string) error {
	enums, err := e.reader.ListEnumerations()
	if err != nil {
		return fmt.Errorf("failed to list enumerations: %w", err)
	}

	// Get hierarchy for module/folder resolution
	h, err := e.getHierarchy()
	if err != nil {
		return fmt.Errorf("failed to build hierarchy: %w", err)
	}

	// Collect rows and calculate column widths
	type row struct {
		qualifiedName string
		module        string
		name          string
		folderPath    string
		values        int
	}
	var rows []row
	qnWidth := len("Qualified Name")
	modWidth := len("Module")
	nameWidth := len("Name")
	pathWidth := len("Folder")
	valWidth := len("Values")

	for _, enum := range enums {
		modID := h.FindModuleID(enum.ContainerID)
		modName := h.GetModuleName(modID)
		if moduleName == "" || modName == moduleName {
			qualifiedName := modName + "." + enum.Name
			folderPath := h.BuildFolderPath(enum.ContainerID)

			rows = append(rows, row{qualifiedName, modName, enum.Name, folderPath, len(enum.Values)})
			if len(qualifiedName) > qnWidth {
				qnWidth = len(qualifiedName)
			}
			if len(modName) > modWidth {
				modWidth = len(modName)
			}
			if len(enum.Name) > nameWidth {
				nameWidth = len(enum.Name)
			}
			if len(folderPath) > pathWidth {
				pathWidth = len(folderPath)
			}
			valStr := fmt.Sprintf("%d", len(enum.Values))
			if len(valStr) > valWidth {
				valWidth = len(valStr)
			}
		}
	}

	// Sort by qualified name
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].qualifiedName) < strings.ToLower(rows[j].qualifiedName)
	})

	// Markdown table with aligned columns
	fmt.Fprintf(e.output, "| %-*s | %-*s | %-*s | %-*s | %-*s |\n",
		qnWidth, "Qualified Name", modWidth, "Module", nameWidth, "Name", pathWidth, "Folder", valWidth, "Values")
	fmt.Fprintf(e.output, "|-%s-|-%s-|-%s-|-%s-|-%s-|\n",
		strings.Repeat("-", qnWidth), strings.Repeat("-", modWidth), strings.Repeat("-", nameWidth),
		strings.Repeat("-", pathWidth), strings.Repeat("-", valWidth))
	for _, r := range rows {
		fmt.Fprintf(e.output, "| %-*s | %-*s | %-*s | %-*s | %-*d |\n",
			qnWidth, r.qualifiedName, modWidth, r.module, nameWidth, r.name, pathWidth, r.folderPath, valWidth, r.values)
	}
	fmt.Fprintf(e.output, "\n(%d enumerations)\n", len(rows))
	return nil
}

// describeEnumeration handles DESCRIBE ENUMERATION command.
func (e *Executor) describeEnumeration(name ast.QualifiedName) error {
	enums, err := e.reader.ListEnumerations()
	if err != nil {
		return fmt.Errorf("failed to list enumerations: %w", err)
	}

	h, err := e.getHierarchy()
	if err != nil {
		return fmt.Errorf("failed to build hierarchy: %w", err)
	}

	for _, enum := range enums {
		modID := h.FindModuleID(enum.ContainerID)
		modName := h.GetModuleName(modID)
		if enum.Name == name.Name && (name.Module == "" || modName == name.Module) {
			// Output JavaDoc documentation if present
			if enum.Documentation != "" {
				fmt.Fprintf(e.output, "/**\n * %s\n */\n", enum.Documentation)
			}

			fmt.Fprintf(e.output, "CREATE ENUMERATION %s.%s (\n", modName, enum.Name)
			for i, v := range enum.Values {
				comma := ","
				if i == len(enum.Values)-1 {
					comma = ""
				}
				caption := ""
				if v.Caption != nil {
					caption = v.Caption.GetTranslation("en_US")
				}
				fmt.Fprintf(e.output, "  %s '%s'%s\n", v.Name, caption, comma)
			}
			fmt.Fprintln(e.output, ");")
			fmt.Fprintln(e.output, "/")
			return nil
		}
	}

	return fmt.Errorf("enumeration not found: %s", name)
}

// mendixReservedWords contains words that cannot be used as enumeration value names.
// These are Java reserved words plus Mendix-specific reserved identifiers.
// Using any of these triggers CE7247: "The name 'X' is a reserved word."
var mendixReservedWords = map[string]bool{
	// Java reserved words
	"abstract": true, "assert": true, "boolean": true, "break": true,
	"byte": true, "case": true, "catch": true, "char": true,
	"class": true, "const": true, "continue": true, "default": true,
	"do": true, "double": true, "else": true, "enum": true,
	"extends": true, "false": true, "final": true, "finally": true,
	"float": true, "for": true, "goto": true, "if": true,
	"implements": true, "import": true, "instanceof": true, "int": true,
	"interface": true, "long": true, "native": true, "new": true,
	"null": true, "package": true, "private": true, "protected": true,
	"public": true, "return": true, "short": true, "static": true,
	"strictfp": true, "super": true, "switch": true, "synchronized": true,
	"this": true, "throw": true, "throws": true, "transient": true,
	"true": true, "try": true, "void": true, "volatile": true,
	"while": true,
	// Mendix-specific reserved identifiers
	"changedby": true, "changeddate": true, "con": true, "context": true,
	"createddate": true, "currentuser": true, "empty": true, "guid": true,
	"id": true, "mendixobject": true, "object": true, "owner": true,
	"submetaobjectname": true, "type": true,
}

// ValidateEnumeration checks enumeration value names for reserved words.
// Returns a list of error messages (CE7247 equivalent).
// This function does not require a project connection.
func ValidateEnumeration(stmt *ast.CreateEnumerationStmt) []string {
	var errors []string
	for _, v := range stmt.Values {
		if mendixReservedWords[strings.ToLower(v.Name)] {
			errors = append(errors, fmt.Sprintf(
				"enumeration value '%s' is a reserved word (CE7247). "+
					"Rename to a non-reserved name (e.g., '%s_' or 'Is%s')",
				v.Name, v.Name, v.Name))
		}
	}
	return errors
}
