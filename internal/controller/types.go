package controller

// TemplateResource definisce una singola risorsa da renderizzare.
// Corrisponde a un singolo elemento nell'array della ConfigMap.
type TemplateResource struct {
	Name     string          `yaml:"name"`
	Template string          `yaml:"template"`
	Values   []TemplateValue `yaml:"values"`
}

// TemplateValue definisce un valore da iniettare nel template.
type TemplateValue struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"` // Puoi cambiarlo in interface{} se hai bisogno di tipi diversi (es. numeri, booleani)
}
