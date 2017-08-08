package provider

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/rackn/terraform-provider-drp/client"
)

// This function doesn't really *create* a new machine but, power an already registered
// machine.
func resourceDRPInstanceCreate(d *schema.ResourceData, meta interface{}) error {
	log.Println("[DEBUG] [resourceDRPInstanceCreate] Launching new drp_instance")
	cc := meta.(*client.Client)

	constraints, err := parseConstraints(d)
	if err != nil {
		log.Println("[ERROR] [resourceDRPInstanceCreate] Unable to parse constraints.")
		return err
	}

	machineObj, err := cc.AllocateMachine(constraints)
	if err != nil {
		log.Println("[ERROR] [resourceDRPInstanceCreate] Unable to allocate machine: %v", err)
		return err
	}

	// Update the machine to request position
	err = cc.UpdateMachine(machineObj, constraints)
	if err != nil {
		log.Println("[ERROR] [resourceDRPInstanceCreate] Unable to initialize machine: %v", err)
		if err2 := cc.ReleaseMachine(machineObj.UUID()); err2 != nil {
			log.Println("[ERROR] [resourceDRPInstanceCreate] Unable to release machine: %v", err2)
		}
		return err
	}

	if err := cc.MachineDo(machineObj.UUID(), "power_on", url.Values{}); err != nil {
		log.Printf("[ERROR] [resourceDRPInstanceCreate] Unable to power up machine: %s\n", machineObj.UUID())
		if err2 := cc.ReleaseMachine(machineObj.UUID()); err2 != nil {
			log.Println("[ERROR] [resourceDRPInstanceCreate] Unable to release machine: %v", err2)
		}
		return err
	}

	log.Printf("[DEBUG] [resourceDRPInstanceCreate] Waiting for instance (%s) to become active\n", machineObj.UUID())
	stateConf := &resource.StateChangeConf{
		Pending:    []string{"9:"},
		Target:     []string{"6:"},
		Refresh:    cc.GetMachineStatus(machineObj.UUID()),
		Timeout:    25 * time.Minute,
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	if _, err := stateConf.WaitForState(); err != nil {
		if err2 := cc.ReleaseMachine(machineObj.UUID()); err2 != nil {
			log.Println("[ERROR] [resourceDRPInstanceCreate] Unable to release machine: %v", err2)
		}
		return fmt.Errorf(
			"[ERROR] [resourceDRPInstanceCreate] Error waiting for instance (%s) to become deployed: %s",
			machineObj.UUID(), err)
	}

	d.SetId(machineObj.UUID())
	return resourceDRPInstanceUpdate(d, meta)
}

func resourceDRPInstanceRead(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] Reading instance (%s) information.\n", d.Id())
	return nil
}

func resourceDRPInstanceUpdate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG] [resourceDRPInstanceUpdate] Modifying instance %s\n", d.Id())

	d.Partial(true)

	d.Partial(false)

	log.Printf("[DEBUG] Done Modifying instance %s", d.Id())
	return resourceDRPInstanceRead(d, meta)
}

// This function doesn't really *delete* a drp managed instance but releases (read, turns off) the machine.
func resourceDRPInstanceDelete(d *schema.ResourceData, meta interface{}) error {
	cc := meta.(*client.Client)
	log.Printf("[DEBUG] Deleting instance %s\n", d.Id())

	if err := cc.ReleaseMachine(d.Id()); err != nil {
		return err
	}

	stateConf := &resource.StateChangeConf{
		Pending:    []string{"6:"},
		Target:     []string{"4:"},
		Refresh:    cc.GetMachineStatus(d.Id()),
		Timeout:    10 * time.Minute,
		Delay:      10 * time.Second,
		MinTimeout: 3 * time.Second,
	}

	if _, err := stateConf.WaitForState(); err != nil {
		return fmt.Errorf(
			"[ERROR] [resourceDRPInstanceCreate] Error waiting for instance (%s) to become ready: %s",
			d.Id(), err)
	}

	log.Printf("[DEBUG] [resourceDRPInstanceDelete] Machine (%s) released", d.Id())

	d.SetId("")

	return nil
}

var stringParams = []string{
	"name",
	"bootenv",
	"owner",
	"description",
}

func parseConstraints(d *schema.ResourceData) (url.Values, error) {
	log.Println("[DEBUG] [parseConstraints] Parsing any existing DRP constraints")
	retVal := url.Values{}

	for _, s := range stringParams {
		sval, set := d.GetOk(s)
		if set {
			log.Printf("[DEBUG] [parseConstraints] setting %s to %+v", s, sval)
			retVal[s] = strings.Fields(sval.(string))
		}
	}

	retVal["profiles"] = []string{}
	aval, set := d.GetOk("profiles")
	if set {
		for _, p := range aval.([]interface{}) {
			retVal["profiles"] = append(retVal["profiles"], p.(string))
		}
	}

	retVal["parameters"] = []string{}
	pval, set := d.GetOk("parameters")
	if set {
		for _, o := range pval.([]interface{}) {
			v := o.(map[string]interface{})
			name := v["name"]
			value := v["value"].(string)
			retVal["parameters"] = append(retVal["parameters"], fmt.Sprintf("%s=%s", name, value))
		}
	}

	retVal["filters"] = []string{}
	pval, set = d.GetOk("filters")
	if set {
		for _, o := range pval.([]interface{}) {
			v := o.(map[string]interface{})
			name := v["name"]
			value := v["value"].(string)
			retVal["filters"] = append(retVal["filters"], fmt.Sprintf("%s=%s", name, value))
		}
	}

	return retVal, nil
}

func resourceDRPInstance() *schema.Resource {
	log.Println("[DEBUG] [resourceDRPInstance] Initializing data structure")
	return &schema.Resource{
		Create: resourceDRPInstanceCreate,
		Read:   resourceDRPInstanceRead,
		Update: resourceDRPInstanceUpdate,
		Delete: resourceDRPInstanceDelete,

		SchemaVersion: 1,

		Schema: map[string]*schema.Schema{
			"bootenv": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"owner": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"name": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"description": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"filters": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"value": {
							Type:     schema.TypeString,
							Optional: true,
						},
					},
				},
			},

			"profiles": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},

			"parameters": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"value": {
							Type:     schema.TypeString,
							Optional: true,
						},
					},
				},
			},
		},
	}
}