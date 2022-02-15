import Ember from 'ember';
import decode from 'npm:jwt-decode';
import config from '../config/environment';
const ID_TOKEN_KEY = 'token';

export default Ember.Service.extend({
    login(login,password) {
      var url = config.APP.ApiUrl + 'signin';
      return Ember.$.ajax({
        method: "POST",
        url: url,
        dataType: 'json',
        data: JSON.stringify({
          username: login,
          password: password,
        })
      }).then((result) => {
        console.log(result);
        this.setIdToken(result.token);
        Ember.$.cookie('access-token', result.token);
      });
    },

    logout() {
        this.clearIdToken();
        window.location.href = "/";
    },

    getIdToken() {
        return localStorage.getItem(ID_TOKEN_KEY);
    },

    clearIdToken() {
        localStorage.removeItem(ID_TOKEN_KEY);
    },

    // Helper function that will allow us to extract the access_token and id_token
    getParameterByName(name) {
        let match = RegExp('[#&]' + name + '=([^&]*)').exec(window.location.href);
        return match && decodeURIComponent(match[1].replace(/\+/g, ' '));
    },

    // Get and store id_token in local storage
    setIdToken(idToken) {
        localStorage.setItem(ID_TOKEN_KEY, idToken);
        this.set(ID_TOKEN_KEY, idToken);
    },

    isLoggedIn() {
        const idToken = this.getIdToken();
        return !!idToken && !this.isTokenExpired(idToken);
    },

    getTokenExpirationDate(encodedToken) {
        const token = decode(encodedToken);
        if (!token.exp) { return null; }

        const date = new Date(0);
        date.setUTCSeconds(token.exp);

        return date;
    },

    isTokenExpired(token) {
        const expirationDate = this.getTokenExpirationDate(token);
        return expirationDate < new Date();
    }
});
