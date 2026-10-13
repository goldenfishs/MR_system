import { createApp } from "vue";
import { createVuetify } from "vuetify";
import * as components from "vuetify/components";
import * as directives from "vuetify/directives";
import { zhHans } from "vuetify/locale";
import "vuetify/styles";
import "@mdi/font/css/materialdesignicons.css";
import "./style.css";
import App from "./App.vue";
import router from "./router";
const vuetify = createVuetify({
  display: { mobileBreakpoint: 960 },
  components,
  directives,
  locale: { locale: "zhHans", messages: { zhHans } },
  theme: {
    defaultTheme: "team",
    themes: {
      team: {
        dark: false,
        colors: {
          primary: "#1976d2",
          secondary: "#7b1fa2",
          success: "#43a047",
          warning: "#f39c28",
          error: "#e53935",
          info: "#2196f3",
          background: "#f5f5f5",
          surface: "#ffffff",
        },
      },
    },
  },
  defaults: {
    VBtn: {
      elevation: 0,
      rounded: "sm",
      style: "text-transform:none;letter-spacing:0",
    },
    VCard: { elevation: 1, rounded: "sm", border: true },
    VTextField: {
      variant: "underlined",
      density: "comfortable",
      color: "primary",
    },
    VSelect: {
      variant: "underlined",
      density: "comfortable",
      color: "primary",
    },
    VTextarea: { variant: "underlined", color: "primary" },
    VChip: { rounded: true, size: "small" },
  },
});
createApp(App).use(router).use(vuetify).mount("#app");
