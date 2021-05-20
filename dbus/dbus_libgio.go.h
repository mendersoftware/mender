// exported by golang, see dbus_libgio.go
GVariant *handle_method_call_callback(
    gchar *objectPath, gchar *interfaceName, gchar *methodName);

// convert an unsafe pointer to a GDBusConnection structure
static GDBusConnection *to_gdbusconnection(void *ptr)
{
    return (GDBusConnection *)ptr;
}

// convert an unsafe pointer to a GMainLoop structure
static GMainLoop *to_gmainloop(void *ptr)
{
    return (GMainLoop *)ptr;
}

// create a new GVariant from a string value
static GVariant *g_variant_new_from_string(gchar *value)
{
    return g_variant_new("(s)", value);
}

// create a new GVariant from two string values
static GVariant *g_variant_new_from_two_strings(gchar *value1, gchar *value2)
{
    return g_variant_new("(ss)", value1, value2);
}

// create a new GVariant from a boolean valule
static GVariant *g_variant_new_from_boolean(gboolean value)
{
    return g_variant_new("(b)", value);
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
    GVariant *response = handle_method_call_callback(
        (char *)object_path, (char *)interface_name, (char *)method_name);
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