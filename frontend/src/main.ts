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
  components,
  directives,
  locale: { locale: "zhHans", messages: { zhHans } },
  theme: {
    defaultTheme: "team",
    themes: {
      team: {
        dark: false,
        colors: {
          primary: "#4959bd",
          secondary: "#277e70",
          success: "#268570",
          warning: "#c38736",
          error: "#cf5a61",
          info: "#526db7",
          background: "#f5f6fa",
          surface: "#ffffff",
        },
      },
    },
  },
  defaults: {
    VBtn: {
      elevation: 0,
      rounded: "lg",
      style: "text-transform:none;letter-spacing:0",
    },
    VCard: { elevation: 0, rounded: "xl", border: true },
    VTextField: {
      variant: "outlined",
      density: "comfortable",
      color: "primary",
    },
    VSelect: { variant: "outlined", density: "comfortable", color: "primary" },
    VTextarea: { variant: "outlined", color: "primary" },
    VChip: { rounded: "lg", size: "small" },
  },
});
createApp(App).use(router).use(vuetify).mount("#app");
