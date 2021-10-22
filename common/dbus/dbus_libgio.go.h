// Copyright 2021 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

// exported by golang, see dbus_libgio.go
GVariant *handle_method_call_callback(
    gchar *objectPath,
    gchar *interfaceName,
    gchar *methodName,
    gchar *parameter_string,
    gpointer user_data);
void dbusSignalCallback(
    GDBusConnection *connection,
    gchar *senderName,
    gchar *objectPath,
    gchar *interfaceName,
    gchar *signalName,
    GVariant *parameters,
    gpointer userData);

static const gchar *extract_parameter(GVariant *parameters)
{
    if (g_variant_is_of_type(parameters, G_VARIANT_TYPE_STRING))
    {
        return g_variant_get_string(parameters, NULL);
    }
    else if (
        g_variant_is_of_type(parameters, G_VARIANT_TYPE_TUPLE) &&
        g_variant_n_children(parameters) != 0)
    {
        if (g_variant_n_children(parameters) == 1)
        {
            GVariant *tmp = g_variant_get_child_value(parameters, 0);
            if (g_variant_is_of_type(tmp, G_VARIANT_TYPE_STRING))
            {
                return g_variant_get_string(tmp, NULL);
            }
            else
            {
                printf(
                    "Unknown tuple type received: %s\n",
                    g_variant_get_type_string(parameters));
            }
        }
        else
        {
            printf(
                "Received a tuple with %lu values, only 1 value supported: (s)\n",
                g_variant_n_children(parameters));
        }
    }
    return NULL;
}

// handle method call events on registered objects
static void handle_method_call(
    GDBusConnection *connection,
    const gchar *sender,
    const gchar *object_path,
    const gchar *interface_name,
    const gchar *method_name,
    GVariant *parameters,
    GDBusMethodInvocation *invocation,
    gpointer user_data)
{
    const gchar *parameter = extract_parameter(parameters);
    GVariant *response = handle_method_call_callback(
        (char *)object_path,
        (char *)interface_name,
        (char *)method_name,
        (char *)parameter,
        user_data);
    if (response != NULL)
    {
        g_dbus_method_invocation_return_value(invocation, response);
    }
    else
    {
        g_dbus_method_invocation_return_dbus_error(
            invocation,
            "io.mender.Failed",
            "Method returned error, see Mender logs for more details");
    }
}

// handle get property events on registered objects
static GVariant *handle_get_property(
    GDBusConnection *connection,
    const gchar *sender,
    const gchar *object_path,
    const gchar *interface_name,
    const gchar *property_name,
    GError **error,
    gpointer user_data)
{
    return NULL;
}

// handle set property events on registered objects
static gboolean handle_set_property(
    GDBusConnection *connection,
    const gchar *sender,
    const gchar *object_path,
    const gchar *interface_name,
    const gchar *property_name,
    GVariant *value,
    GError **error,
    gpointer user_data)
{
    return FALSE;
}

// global static interface vtable to hook the call method, get and set
// property callbacks
static GDBusInterfaceVTable interface_vtable = {
    handle_method_call, handle_get_property, handle_set_property};

// return the static interface vtable above, as golang cannot access statics
// from C
static GDBusInterfaceVTable *get_interface_vtable()
{
    return (GDBusInterfaceVTable *)&interface_vtable;
}
